#!/usr/bin/env bash
set -euo pipefail

OUT_DIR="${1:-dist}"
ALLOW_CGO_CROSS="${ALLOW_CGO_CROSS:-}" # set to non-empty to allow GUI cross-builds when toolchain exists
HOST_OS=$(go env GOHOSTOS)
HOST_ARCH=$(go env GOHOSTARCH)
mkdir -p "${OUT_DIR}"

# OS/Arch matrix
MATRIX=(
  "windows amd64"
  "windows arm64"
  "linux amd64"
  "linux arm64"
)

PACKAGES=("./server" "./client")

for entry in "${MATRIX[@]}"; do
done
  read -r GOOS GOARCH <<<"${entry}"
  for pkg in "${PACKAGES[@]}"; do
    name=$(basename "${pkg}")
    ext=""
    if [[ "${GOOS}" == "windows" ]]; then
      ext=".exe"
    fi
    out="${OUT_DIR}/${name}-${GOOS}-${GOARCH}${ext}"

    if [[ "${name}" == "server" ]]; then
      CGO_ENABLED=0
    else
      CGO_ENABLED=1
      if [[ -z "${ALLOW_CGO_CROSS}" && "${GOOS}" != "${HOST_OS}" ]]; then
        echo "[skip ] ${out} (GUI cross-build needs target C toolchain; set ALLOW_CGO_CROSS=1 once installed)" >&2
        continue
      fi
    fi

    echo "[build] ${out}"
    GOOS="${GOOS}" GOARCH="${GOARCH}" CGO_ENABLED=${CGO_ENABLED} go build -o "${out}" "${pkg}"
  done


echo "Artifacts are in ${OUT_DIR}/"
