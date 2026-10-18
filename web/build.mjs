// Bundles the UI into web/dist (embedded into iosview.exe by Go).
import { build } from "esbuild";
import { copyFileSync, mkdirSync, rmSync } from "node:fs";
rmSync("dist", { recursive: true, force: true });
mkdirSync("dist", { recursive: true });
await build({
  entryPoints: ["src/main.ts"], bundle: true, format: "esm", target: "es2022",
  minify: true, sourcemap: false, outfile: "dist/app.js", legalComments: "eof",
});
copyFileSync("src/index.html", "dist/index.html");
copyFileSync("src/style.css", "dist/style.css");
copyFileSync("src/icon.svg", "dist/icon.svg");
copyFileSync("src/icon.png", "dist/icon.png");
console.log("UI built -> web/dist");
