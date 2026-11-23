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
copyFileSync("src/inter.woff2", "dist/inter.woff2");
for (const n of [20, 40, 48, 64]) copyFileSync(`src/tb-${n}.png`, `dist/tb-${n}.png`);   // title bar logo = .ico frames
console.log("UI built -> web/dist");
