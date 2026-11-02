// Renders the corpus through node-tikzjax configured as the desktop's tikz.ts does.
const fs = require("fs");
const path = require("path");
const mod = require("/Users/adibhanna/Developer/opensource/zennotes/node_modules/node-tikzjax");
const tex2svg = typeof mod === "function" ? mod : (mod.default.default || mod.default);
const [corpusPath, out, onlyId] = process.argv.slice(2);
const corpus = JSON.parse(fs.readFileSync(corpusPath, "utf8"));
const LIBS = "arrows.meta,calc,positioning,shapes,decorations.pathreplacing,intersections,patterns";

// Desktop wrapSource (apps/desktop/src/main/tikz.ts).
function wrapSource(source) {
  const trimmed = source.trim();
  const noClass = trimmed.replace(/^\s*\\documentclass(?:\[[^\]]*])?\{[^}]+\}\s*$/gm, "").trim();
  if (/\\begin\{document\}/.test(noClass) && /\\end\{document\}/.test(noClass)) return noClass;
  const bare = noClass.replace(/\\begin\{document\}/g, "").replace(/\\end\{document\}/g, "").trim();
  const cands = [/\\begin\{(?!document\b)[^}]+\}/, /\\tikz\b/, /\\draw\b/, /\\path\b/, /\\node\b/, /\\coordinate\b/, /\\matrix\b/, /\\graph\b/];
  let at = -1;
  for (const p of cands) { const m = p.exec(bare); if (m && (at < 0 || m.index < at)) at = m.index; }
  if (at > 0) return `${bare.slice(0, at).trim()}\n\\begin{document}\n${bare.slice(at).trim()}\n\\end{document}`;
  return `\\begin{document}\n${bare}\n\\end{document}`;
}
function mathDoc(src) {
  let s = src.trim();
  if (s.startsWith("$$") && s.endsWith("$$")) s = "\\[ " + s.slice(2, -2).trim() + " \\]";
  return `\\begin{document}\n\\begin{tikzpicture}\\node[inner sep=0pt]{\\begin{minipage}{14cm}\n${s}\n\\end{minipage}};\\end{tikzpicture}\n\\end{document}`;
}
(async () => {
  const results = [];
  for (const group of ["tikz", "math"]) {
    for (const it of corpus[group]) {
      if (onlyId && it.id !== onlyId) continue;
      const texPackages = { amsmath: "", amssymb: "" };
      if (it.src.includes("pgfplots") || it.src.includes("axis") || it.src.includes("plot")) texPackages.pgfplots = "";
      if (it.src.includes("circuitikz")) texPackages.circuitikz = "";
      if (group === "math" && it.src.includes("\\ce")) texPackages.mhchem = "version=4";
      const input = group === "tikz" ? wrapSource(it.src) : mathDoc(it.src);
      const start = Date.now();
      let r = { ID: it.id, Cat: it.cat, Kind: group, OK: false, Err: "", Ms: 0 };
      const logs = [];
      const orig = console.log;
      console.log = (...a) => logs.push(a.join(" "));
      try {
        const svg = await tex2svg(input, { showConsole: true, texPackages, tikzLibraries: LIBS });
        r.OK = true;
        fs.writeFileSync(path.join(out, it.id + ".svg"), svg);
      } catch (e) {
        const bang = logs.filter((l) => l.startsWith("!"));
        r.Err = (bang[0] || String(e && e.message || e)).slice(0, 200);
      } finally {
        console.log = orig;
      }
      r.Ms = Date.now() - start;
      results.push(r);
    }
  }
  if (onlyId) { fs.writeFileSync(path.join(out, onlyId + ".json"), JSON.stringify(results[0])); return; }
  fs.writeFileSync(path.join(out, "results.json"), JSON.stringify(results, null, 1));
  console.log(results.length, "rendered");
})();
