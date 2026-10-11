#include "foray_core_impl.h"

#include <arpa/inet.h>
#include <cctype>
#include <cerrno>
#include <cstdlib>
#include <cstring>
#include <fcntl.h>
#include <fstream>
#include <map>
#include <mutex>
#include <netinet/in.h>
#include <poll.h>
#include <sstream>
#include <sys/socket.h>
#include <sys/stat.h>
#include <thread>
#include <unistd.h>
#include <vector>

namespace {

constexpr int kPort = 7389;
constexpr size_t kMaxBody = 8 << 20;

std::string jsonEscape(const std::string& s)
{
    std::string out;
    for (unsigned char c : s) {
        switch (c) {
        case '"': out += "\\\""; break;
        case '\\': out += "\\\\"; break;
        case '\n': out += "\\n"; break;
        case '\r': out += "\\r"; break;
        case '\t': out += "\\t"; break;
        default:
            if (c < 0x20) {
                char b[7];
                std::snprintf(b, sizeof b, "\\u%04x", c);
                out += b;
            } else {
                out += static_cast<char>(c);
            }
        }
    }
    return out;
}

std::string errorJson(const std::string& what)
{
    return "{\"error\":\"" + jsonEscape(what) + "\"}";
}

// A shrooms mesh address: fd00::/8, or the IPv4 aliases in 198.18.0.0/15.
bool isMeshAddress(const std::string& a)
{
    unsigned char b[16];
    if (inet_pton(AF_INET6, a.c_str(), b) == 1) return b[0] == 0xfd;
    if (inet_pton(AF_INET, a.c_str(), b) == 1) return b[0] == 198 && (b[1] == 18 || b[1] == 19);
    return false;
}

// Only the lists API, and nothing that could leave the request line.
bool safePath(const std::string& p)
{
    if (p.compare(0, 9, "/v1/lists") != 0 || p.size() > 512) return false;
    for (unsigned char c : p) {
        if (c <= 0x20 || c >= 0x7f) return false;
    }
    return p.find("..") == std::string::npos;
}

int dial(const std::string& address, int timeoutSec, std::string& err)
{
    sockaddr_storage ss{};
    socklen_t len = 0;
    int family = AF_INET6;
    auto* v6 = reinterpret_cast<sockaddr_in6*>(&ss);
    auto* v4 = reinterpret_cast<sockaddr_in*>(&ss);
    if (inet_pton(AF_INET6, address.c_str(), &v6->sin6_addr) == 1) {
        v6->sin6_family = AF_INET6;
        v6->sin6_port = htons(kPort);
        len = sizeof(sockaddr_in6);
    } else {
        family = AF_INET;
        inet_pton(AF_INET, address.c_str(), &v4->sin_addr);
        v4->sin_family = AF_INET;
        v4->sin_port = htons(kPort);
        len = sizeof(sockaddr_in);
    }
    int fd = ::socket(family, SOCK_STREAM, 0);
    if (fd < 0) {
        err = std::string("socket: ") + std::strerror(errno);
        return -1;
    }
    int flags = ::fcntl(fd, F_GETFL, 0);
    ::fcntl(fd, F_SETFL, flags | O_NONBLOCK);
    int rc = ::connect(fd, reinterpret_cast<sockaddr*>(&ss), len);
    if (rc < 0 && errno != EINPROGRESS) {
        err = "cannot reach " + address + ": " + std::strerror(errno);
        ::close(fd);
        return -1;
    }
    if (rc < 0) {
        pollfd p{fd, POLLOUT, 0};
        if (::poll(&p, 1, timeoutSec * 1000) <= 0) {
            err = "cannot reach " + address;
            ::close(fd);
            return -1;
        }
        int soErr = 0;
        socklen_t sl = sizeof soErr;
        ::getsockopt(fd, SOL_SOCKET, SO_ERROR, &soErr, &sl);
        if (soErr != 0) {
            err = "cannot reach " + address + ": " + std::strerror(soErr);
            ::close(fd);
            return -1;
        }
    }
    ::fcntl(fd, F_SETFL, flags);
    timeval tv{};
    tv.tv_sec = timeoutSec;
    ::setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof tv);
    ::setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, sizeof tv);
    return fd;
}

std::string unchunk(const std::string& in)
{
    std::string out;
    size_t i = 0;
    while (i < in.size()) {
        auto eol = in.find("\r\n", i);
        if (eol == std::string::npos) break;
        long n = std::strtol(in.substr(i, eol - i).c_str(), nullptr, 16);
        if (n <= 0) break;
        out.append(in, eol + 2, static_cast<size_t>(n));
        i = eol + 2 + static_cast<size_t>(n) + 2;
    }
    return out;
}

// One request; the body of a 2xx, or false with the service's own words.
bool request(const std::string& address, const std::string& method, const std::string& path,
             const std::string& body, int timeoutSec, std::string& out, std::string& err)
{
    if (!isMeshAddress(address)) {
        err = address + " is not a mesh address";
        return false;
    }
    if (!safePath(path)) {
        err = "not a Foray path";
        return false;
    }
    int fd = dial(address, timeoutSec, err);
    if (fd < 0) return false;
    std::string host = address.find(':') != std::string::npos ? "[" + address + "]" : address;
    std::string req = method + " " + path + " HTTP/1.0\r\nHost: " + host + ":" + std::to_string(kPort) +
                      "\r\nConnection: close\r\n";
    if (method != "GET") {
        req += "Content-Type: application/json\r\nContent-Length: " + std::to_string(body.size()) + "\r\n";
    }
    req += "\r\n" + body;
    size_t sent = 0;
    while (sent < req.size()) {
        ssize_t n = ::send(fd, req.data() + sent, req.size() - sent, MSG_NOSIGNAL);
        if (n <= 0) {
            err = std::string("write: ") + std::strerror(errno);
            ::close(fd);
            return false;
        }
        sent += static_cast<size_t>(n);
    }
    std::string raw;
    char buf[8192];
    for (;;) {
        ssize_t n = ::recv(fd, buf, sizeof buf, 0);
        if (n < 0) {
            err = std::string("read: ") + std::strerror(errno);
            ::close(fd);
            return false;
        }
        if (n == 0) break;
        raw.append(buf, static_cast<size_t>(n));
        if (raw.size() > kMaxBody) {
            err = "the reply is implausibly large";
            ::close(fd);
            return false;
        }
    }
    ::close(fd);
    auto sep = raw.find("\r\n\r\n");
    if (sep == std::string::npos || raw.compare(0, 5, "HTTP/") != 0) {
        err = "the reply is not HTTP";
        return false;
    }
    std::string headers = raw.substr(0, sep);
    std::string payload = raw.substr(sep + 4);
    for (auto& c : headers) c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
    if (headers.find("transfer-encoding: chunked") != std::string::npos) payload = unchunk(payload);
    auto sp = raw.find(' ');
    std::string code = sp == std::string::npos ? "" : raw.substr(sp + 1, 3);
    if (code.empty() || code[0] != '2') {
        auto k = payload.find("\"error\":\"");
        std::string why = k == std::string::npos ? payload.substr(0, 200)
                                                  : payload.substr(k + 9, payload.find('"', k + 9) - k - 9);
        err = why.empty() ? "the service answered " + code : why;
        return false;
    }
    out = payload.empty() ? "{}" : payload;
    return true;
}

// --- finding lists ------------------------------------------------------------

struct Finder {
    std::mutex mu;
    std::map<std::string, std::string> byAddress;  // address -> JSON entry
    unsigned generation = 0;
};

Finder& finder()
{
    static Finder f;
    return f;
}

std::vector<std::string> split(const std::string& s, char sep)
{
    std::vector<std::string> out;
    std::stringstream ss(s);
    std::string part;
    while (std::getline(ss, part, sep)) out.push_back(part);
    return out;
}

std::string foundJson()
{
    Finder& f = finder();
    std::lock_guard<std::mutex> lock(f.mu);
    std::string out = "[";
    bool first = true;
    for (auto& kv : f.byAddress) {
        if (!first) out += ",";
        out += kv.second;
        first = false;
    }
    return out + "]";
}

std::string prefPath()
{
    const char* xdg = std::getenv("XDG_CONFIG_HOME");
    std::string base;
    if (xdg && *xdg) {
        base = xdg;
    } else {
        const char* home = std::getenv("HOME");
        if (!home || !*home) return "";
        base = std::string(home) + "/.config";
    }
    base += "/foray";
    ::mkdir(base.c_str(), 0700);
    return base + "/view.conf";
}

bool validKey(const std::string& k)
{
    if (k.empty() || k.size() > 64) return false;
    for (unsigned char c : k) {
        if (!std::isalnum(c) && c != '_' && c != '-' && c != '.') return false;
    }
    return true;
}

}  // namespace

std::string ForayCoreImpl::find(const std::string& peers)
{
    Finder& f = finder();
    unsigned gen;
    {
        std::lock_guard<std::mutex> lock(f.mu);
        gen = ++f.generation;
    }
    for (const auto& entry : split(peers, ';')) {
        auto parts = split(entry, '|');
        if (parts.size() < 3 || !isMeshAddress(parts[2])) continue;
        std::string name = parts[0], mesh = parts[1], address = parts[2];
        // One thread per peer: a household mesh is a handful of machines,
        // and one that does not answer must not hold up the rest.
        std::thread([name, mesh, address, gen]() {
            std::string out, err;
            bool ok = request(address, "GET", "/v1/lists", "", 3, out, err);
            Finder& f = finder();
            std::lock_guard<std::mutex> lock(f.mu);
            if (!ok) {
                f.byAddress.erase(address);
                return;
            }
            f.byAddress[address] = "{\"address\":\"" + jsonEscape(address) + "\",\"peer\":\"" + jsonEscape(name) +
                                   "\",\"mesh\":\"" + jsonEscape(mesh) + "\",\"reply\":" + out + "}";
            (void)gen;
        }).detach();
    }
    return foundJson();
}

std::string ForayCoreImpl::found()
{
    return foundJson();
}

std::string ForayCoreImpl::get(const std::string& address, const std::string& path)
{
    std::string out, err;
    if (!request(address, "GET", path, "", 4, out, err)) return errorJson(err);
    return out;
}

std::string ForayCoreImpl::send(const std::string& address, const std::string& method,
                                const std::string& path, const std::string& body)
{
    if (method != "POST" && method != "PATCH") return errorJson("POST or PATCH");
    std::string out, err;
    if (!request(address, method, path, body, 6, out, err)) return errorJson(err);
    return out;
}

std::string ForayCoreImpl::getPref(const std::string& key)
{
    if (!validKey(key)) return "";
    std::ifstream f(prefPath());
    std::string line;
    while (std::getline(f, line)) {
        auto eq = line.find('=');
        if (eq != std::string::npos && line.substr(0, eq) == key) return line.substr(eq + 1);
    }
    return "";
}

std::string ForayCoreImpl::setPref(const std::string& key, const std::string& value)
{
    if (!validKey(key) || value.find('\n') != std::string::npos) return errorJson("not a preference");
    std::map<std::string, std::string> all;
    {
        std::ifstream f(prefPath());
        std::string line;
        while (std::getline(f, line)) {
            auto eq = line.find('=');
            if (eq != std::string::npos) all[line.substr(0, eq)] = line.substr(eq + 1);
        }
    }
    if (value.empty()) all.erase(key); else all[key] = value;
    std::string path = prefPath(), tmp = path + ".tmp";
    {
        std::ofstream f(tmp, std::ios::trunc);
        for (auto& kv : all) f << kv.first << "=" << kv.second << "\n";
    }
    if (std::rename(tmp.c_str(), path.c_str()) != 0) return errorJson("cannot save");
    return "{\"ok\":true}";
}
