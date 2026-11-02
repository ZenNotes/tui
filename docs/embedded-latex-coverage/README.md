# Embedded LaTeX: coverage audit (2026-10-27, parked)

Question: could zn render `math = "latex"` without the user installing TeX,
and would users lose anything? Decision: **not now**. zn keeps using the
user's own `pdftex` and `pdftocairo`; this folder keeps the evidence for a
later look.

## Method

`harness/corpus.py` writes `harness/corpus.json`: 90 math and 34 TikZ cases
covering every display form zn detects, amsmath, amssymb, mhchem, macros
defined in a block, colour, TikZ with each default preamble library, and
fences that add their own `\usetikzlibrary` or `\usepackage`. Each case ran
through:

- zn 0.12.0's own latex path (`latexPNG`, TeX Live 2026 basic plus the
  README's `tlmgr` list, in Docker): the baseline, 120 of 124 render.
- `github.com/d2lang/mathjax-go` v0.1.0 (math only).
- node-tikzjax 1.0.5, the desktop's TikZ engine (e-TeX in WebAssembly),
  configured as `apps/desktop/src/main/tikz.ts` calls it; math wrapped in a
  TikZ node.

Results per case are in `results/`. Every case was also compared visually
against the TeX picture (pages built from the outputs; regenerate them by
rerunning the harness).

## Results (of what the TeX path renders)

| | Math (86) | TikZ (34) |
|---|---|---|
| mathjax-go | 68 | none |
| tikzjax | 75 | 33 |
| either | 83 | 33 |

- **mathjax-go** misses `\newcommand`/`\renewcommand`/`\def`, `eqnarray`,
  `subequations`, `\intertext`, `\hline` in arrays, `\pmb`, `\pod`,
  `\mathstrut`, `\leftroot`, `\overleftrightarrow`, mhchem `<=>>` and `&`
  inside `\ce`. Silent differences: no automatic equation numbers, wrong
  `alignat` spacing, literal `$` inside `\colorbox`. About 0.16 ms a block.
  oksvg rendered none of its SVG.
- **tikzjax** misses mhchem (not in its bundle), crashes on `\pmb` and
  `\boldsymbol` (missing font `cmmib5`; the throw escapes its promise and
  kills the Node process, which matters for the desktop too), and breaks on
  `\substack`. Silent differences: `patterns` render empty, `matrix of nodes`
  loses node borders. About 0.36 s a block including Node start-up.
- Nothing embedded covers packages a user adds to `preamble.tex`.

## If this is reopened

The strongest route was one embedded engine (tikzjax's wasm under wazero)
with mhchem and the bold-math fonts added, a Go DVI-to-PNG renderer, and the
installed `pdftex` as automatic fallback for custom preambles. Open
questions: wazero speed, renderer effort, and dvi2html's unclear licence.

## Rerunning

- TeX: build `harness/Dockerfile.texlive` as `zn-texlive-readme`, put
  `harness/pdftex-docker-wrapper.sh` on PATH as `pdftex`, and use
  `harness/pdftex_coverage_test.go.txt`.
- mathjax-go: copy `harness/mathjax-run.go.txt` to `main.go` with
  `mathjax-go.mod`/`.sum` as `go.mod`/`go.sum` in an empty folder;
  `go run . corpus.json <outdir>`. (The `.txt` names keep these files out of
  zn's own module.)
- tikzjax: `node harness/tikzjax-run.cjs corpus.json <outdir> [id]` (needs
  the desktop repo's `node_modules`; run one id per process, a missing font
  kills the process).
