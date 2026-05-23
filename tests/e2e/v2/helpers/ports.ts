import net from "node:net";

// pickFreePort asks the OS for a free loopback port by opening a
// socket on :0 and reading the assigned port back. Races between
// pick + bind are still possible — the agent-center binary surfaces
// "address already in use" if so and the fixture poll loop will
// fail-fast. Pragmatic vs perfect: we accept the tiny race window
// in exchange for not needing a global port-allocator service.
export async function pickFreePort(): Promise<number> {
  return new Promise<number>((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.on("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const addr = srv.address();
      if (typeof addr !== "object" || addr === null) {
        srv.close();
        return reject(new Error("no addr"));
      }
      const port = addr.port;
      srv.close(() => resolve(port));
    });
  });
}
