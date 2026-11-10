// Local-only preview of an already built static site. Not a production server.
import { createServer } from "node:http";
import { createReadStream, existsSync } from "node:fs";
import { readFile, stat } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { extname, resolve, sep } from "node:path";

const root = fileURLToPath(
  new URL("../dist/runloom-website/browser/", import.meta.url),
);
const argument = process.argv.indexOf("--port");
const port = Number(
  argument < 0 ? process.env.PORT || 4301 : process.argv[argument + 1],
);
if (!Number.isInteger(port) || port < 1 || port > 65535)
  throw new Error("Invalid preview port");
if (!existsSync(resolve(root, "index.html")))
  throw new Error("Build the website first: npm run build");
const studio = process.env.RUNLOOM_WEBSITE_STUDIO_URL;
let studioMeta = "";
if (studio) {
  const url = new URL(studio);
  if (url.protocol !== "http:" || !["localhost", "127.0.0.1"].includes(url.hostname) || url.username || url.password || url.pathname !== "/" || url.search || url.hash)
    throw new Error("Preview Studio URL must be an HTTP loopback origin");
  studioMeta = `<meta name="runloom-studio-url" content="${url.origin}/">`;
}
const mime = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".svg": "image/svg+xml",
  ".json": "application/json",
  ".txt": "text/plain; charset=utf-8",
};
const server = createServer(async (request, response) => {
  if (!["GET", "HEAD"].includes(request.method)) {
    response.writeHead(405, { Allow: "GET, HEAD" });
    response.end();
    return;
  }
  try {
    const path = decodeURIComponent(
      new URL(request.url, "http://localhost").pathname,
    );
    const file = resolve(root, "." + (path === "/" ? "/index.html" : path));
    if (
      !file.startsWith(root.endsWith(sep) ? root : root + sep) ||
      !(await stat(file)).isFile()
    )
      throw new Error("Not found");
    response.writeHead(200, {
      "Content-Type": mime[extname(file)] || "text/plain; charset=utf-8",
      "Cache-Control": "no-store",
      "X-Content-Type-Options": "nosniff",
    });
    if (request.method === "HEAD") response.end();
    else if (file === resolve(root, "index.html") && studioMeta)
      response.end((await readFile(file, "utf8")).replace("</head>", `${studioMeta}</head>`));
    else
      createReadStream(file)
        .on("error", () => response.destroy())
        .pipe(response);
  } catch {
    response.writeHead(404);
    response.end("Not found");
  }
});
server.listen(port, "127.0.0.1", () =>
  console.log(`Website preview: http://127.0.0.1:${port}`),
);
server.on("error", (error) => {
  console.error(error.message);
  process.exitCode = 1;
});
for (const signal of ["SIGINT", "SIGTERM"])
  process.on(signal, () => server.close());
