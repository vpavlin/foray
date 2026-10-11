// Agents a list's helpers can be picked from: the sessions on the list's machine and on its peers
// on the list's mesh, as MACHINE/SESSION. Asks each machine's shrooms-agent (port 7387) the way
// Shrooms Agents does. A machine that doesn't answer is skipped; typing an agent always works.
import { api } from "./api";
import { agentBase, baseUrl } from "./model";

export async function findAgents(forayBase: string, mesh: string): Promise<string[]> {
  const home = agentBase(forayBase);
  const out = new Set<string>();
  const sessionsOf = async (machine: string, base: string) => {
    try { for (const s of (await api.sessions(base)).sessions || []) if (s && s.name) out.add(`${machine}/${s.name}`); } catch { /* not reachable */ }
  };
  let machine = "";
  try { machine = (await api.whoami(forayBase)).machine || ""; } catch { /* keep going with peers */ }
  const jobs: Promise<void>[] = [];
  if (machine) jobs.push(sessionsOf(machine, home));
  try {
    const peers = (await api.peers(home)).peers || [];
    for (const p of peers) if (p && p.name && p.overlay && (!mesh || p.mesh === mesh)) jobs.push(sessionsOf(p.name, baseUrl(p.overlay, 7387)));
  } catch { /* no peer list: only this machine's sessions */ }
  await Promise.all(jobs);
  return [...out].sort((a, b) => a.localeCompare(b));
}
