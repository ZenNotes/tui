#!/bin/sh
# Runs the container's pdftex with the work dir, the format dir and $TMPDIR at the same paths.
FMT="${TEXFORMATS%%:*}"
T="${TMPDIR%/}"
set -- -v "$PWD:$PWD" -w "$PWD" ${FMT:+-v "$FMT:$FMT"} ${T:+-v "$T:$T"} -e HOME=/tmp -e openin_any -e openout_any -e TEXFORMATS -e TEXMFOUTPUT zn-texlive-readme pdftex "$@"
exec docker run --rm -i -u "$(id -u):$(id -g)" "$@"
