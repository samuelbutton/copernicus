#!/bin/sh
set -eu

# Run from the repository root. Downloads are pinned and verified before use.
case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) platform=osx-arm64; checksum=37144723eb43639a96b47207b44036e6dedc084f81afbf0c18625e7f3deac810 ;;
  Darwin-x86_64) platform=osx-amd64; checksum=52ba07e03926b19454a627365761041faa2615eaed44afb42ce757d62f625f3d ;;
  Linux-x86_64) platform=linux-amd64; checksum=c61f21485e6e41d3a0c28ce9904ea18346309cf427b4cf9479bc3564348dc885 ;;
  Linux-aarch64) platform=linux-arm64; checksum=50c719e603a4e599d435e5321542458edc1cfc5f7eed65979fe9bc5ae4e3ba23 ;;
  *) echo 'Unsupported DuckDB platform.' >&2; exit 1 ;;
esac
mkdir -p bin
install_dir=$(mktemp -d bin/duckdb-install.XXXXXX)
trap 'rm -rf "$install_dir"' EXIT HUP INT TERM
curl --fail --location --proto '=https' --proto-redir '=https' --connect-timeout 10 --max-time 120 \
  "https://github.com/duckdb/duckdb/releases/download/v1.5.5/duckdb_cli-$platform.gz" -o "$install_dir/duckdb.gz"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$install_dir/duckdb.gz")
else
  actual=$(shasum -a 256 "$install_dir/duckdb.gz")
fi
test "${actual%% *}" = "$checksum" || { echo 'DuckDB checksum mismatch.' >&2; exit 1; }
gzip -dc "$install_dir/duckdb.gz" > "$install_dir/duckdb"
chmod 755 "$install_dir/duckdb"
mv "$install_dir/duckdb" bin/duckdb
echo 'Installed DuckDB v1.5.5 in bin/duckdb.'
