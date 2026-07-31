import http from "node:http";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = path.dirname(fileURLToPath(import.meta.url));
const port = Number(process.env.PORT || 8765);
const bridgeURL = process.env.BRIDGE_URL;
const bridgeToken = process.env.BRIDGE_TOKEN;

http.createServer(async (request, response) => {
  let apiPath = request.method === "GET" && request.url === "/api/summary"
    ? "/desktop/v1/summary"
    : request.method === "GET" && request.url === "/api/accounts"
      ? "/desktop/v1/accounts"
      : request.method === "GET" && request.url === "/api/pools"
        ? "/desktop/v1/pools"
        : request.method === "POST" && request.url === "/api/refresh"
          ? "/desktop/v1/refresh"
      : null;
  const detailMatch = request.method === "GET" && request.url?.match(/^\/api\/accounts\/([a-f0-9]{12})$/);
  if (detailMatch) apiPath = `/desktop/v1/accounts/${detailMatch[1]}`;
  if (apiPath) {
    if (!bridgeURL || !bridgeToken) {
      response.writeHead(503, { "Content-Type": "application/json; charset=utf-8" });
      response.end(JSON.stringify({ error: "Bridge proxy is not configured" }));
      return;
    }
    try {
      const upstream = await fetch(`${bridgeURL}${apiPath}`, {
        method: request.method,
        headers: { Authorization: `Bearer ${bridgeToken}` },
      });
      const content = await upstream.text();
      response.writeHead(upstream.status, {
        "Content-Type": "application/json; charset=utf-8",
        "Cache-Control": "no-store",
        "X-Content-Type-Options": "nosniff",
      });
      response.end(content);
    } catch {
      response.writeHead(502, { "Content-Type": "application/json; charset=utf-8" });
      response.end(JSON.stringify({ error: "Bridge unavailable" }));
    }
    return;
  }
  if (request.url !== "/" && request.url !== "/index.html") {
    response.writeHead(404, { "Content-Type": "text/plain; charset=utf-8" });
    response.end("Not found");
    return;
  }
  try {
    const content = await readFile(path.join(root, "dashboard.html"));
    response.writeHead(200, {
      "Content-Type": "text/html; charset=utf-8",
      "Cache-Control": "no-store",
      "X-Content-Type-Options": "nosniff",
    });
    response.end(content);
  } catch {
    response.writeHead(500, { "Content-Type": "text/plain; charset=utf-8" });
    response.end("Preview unavailable");
  }
}).listen(port, "127.0.0.1", () => {
  console.log(`Sub2 preview: http://127.0.0.1:${port}/`);
});
