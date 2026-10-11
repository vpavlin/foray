#pragma once

#include <string>

#include "logos_module_context.h"

/**
 * @brief Foray lists on the mesh, for the Foray view.
 *
 * A Basecamp view may not open a connection, so this does it: plain HTTP to
 * the Foray service (port 7389) on a machine's mesh address, nothing else —
 * an address that is not on a shrooms mesh (fd00::/8, 198.18.0.0/15) is
 * refused before anything is sent.
 *
 * Calls are short and made on the caller's thread, bounded at a few seconds:
 * the service is on the mesh, and a call that cannot be answered quickly is
 * better reported than waited on. Finding lists asks every peer at once, on a
 * thread of its own, and is polled.
 */
class ForayCoreImpl : public LogosModuleContext
{
public:
    /**
     * @brief Starts asking peers whether they keep lists, and returns what is
     * known so far: `[{"address","peer","mesh","machine","lists":[…]}]`.
     *
     * @param peers "name|mesh|address" entries separated by ";", as the view
     * reads them from shrooms_core's status.
     */
    std::string find(const std::string& peers);

    /** @brief What finding has learned since, the same shape as find(). */
    std::string found();

    /** @brief A GET of a /v1/lists… path on a machine. The service's JSON, or {"error"}. */
    std::string get(const std::string& address, const std::string& path);

    /**
     * @brief A write: POST or PATCH, with a JSON body, of a /v1/lists… path.
     * The service's JSON, or {"error"}.
     */
    std::string send(const std::string& address, const std::string& method,
                     const std::string& path, const std::string& body);

    /** @brief A small view preference, kept beside Basecamp's own. "" when unset. */
    std::string getPref(const std::string& key);
    std::string setPref(const std::string& key, const std::string& value);
};
