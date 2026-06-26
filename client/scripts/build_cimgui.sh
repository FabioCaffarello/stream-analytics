#!/usr/bin/env bash
# Build cimgui (Dear ImGui C wrapper + GLFW/OpenGL3 backends) as a static library.
# Idempotent: skips clone if already present; always recompiles.
# Output: client/deps/imgui/lib/libcimgui.a

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CLIENT_DIR="$(dirname "$SCRIPT_DIR")"
BUILD_TMP="$CLIENT_DIR/.build_cimgui"
OUT_DIR="$CLIENT_DIR/deps/imgui/lib"

CIMGUI_REPO="https://github.com/cimgui/cimgui.git"
CIMGUI_TAG="1.91.8"

# Detect GLFW include path
GLFW_INCLUDE="$(pkg-config --cflags glfw3 2>/dev/null | sed 's/-I//')"
if [ -z "$GLFW_INCLUDE" ]; then
    echo "ERROR: GLFW not found. Install via: brew install glfw"
    exit 1
fi
echo "GLFW include: $GLFW_INCLUDE"

# Detect SDL2 include paths (may be multiple -I flags + defines)
SDL_CFLAGS="$(pkg-config --cflags sdl2 2>/dev/null)"
if [ -z "$SDL_CFLAGS" ]; then
    echo "WARNING: SDL2 not found. SDL2 backend will not be compiled."
    echo "  Install via: brew install sdl2"
    HAS_SDL2=0
else
    echo "SDL2 cflags: $SDL_CFLAGS"
    HAS_SDL2=1
fi

# Clone cimgui if not present
if [ ! -d "$BUILD_TMP/cimgui" ]; then
    echo "Cloning cimgui ($CIMGUI_TAG)..."
    mkdir -p "$BUILD_TMP"
    git clone --recursive --branch "$CIMGUI_TAG" --depth 1 "$CIMGUI_REPO" "$BUILD_TMP/cimgui"
else
    echo "cimgui already cloned, reusing."
fi

SRC="$BUILD_TMP/cimgui"

# Source files to compile
SOURCES=(
    "$SRC/cimgui.cpp"
    "$SRC/imgui/imgui.cpp"
    "$SRC/imgui/imgui_draw.cpp"
    "$SRC/imgui/imgui_tables.cpp"
    "$SRC/imgui/imgui_widgets.cpp"
    "$SRC/imgui/imgui_demo.cpp"
    "$SRC/imgui/backends/imgui_impl_glfw.cpp"
    "$SRC/imgui/backends/imgui_impl_opengl3.cpp"
    "$SCRIPT_DIR/cimgui_impl_bridge.cpp"
)

# Add SDL2 backend sources if available.
if [ "$HAS_SDL2" = "1" ]; then
    SOURCES+=(
        "$SRC/imgui/backends/imgui_impl_sdl2.cpp"
        "$SCRIPT_DIR/cimgui_impl_sdl2_bridge.cpp"
    )
fi

CXXFLAGS=(-O2 -std=c++11)
INCLUDES=("-I$SRC" "-I$SRC/imgui" "-I$SRC/imgui/backends" "-I$GLFW_INCLUDE")

# Add SDL2 include paths if available.
if [ "$HAS_SDL2" = "1" ]; then
    # shellcheck disable=SC2206
    INCLUDES+=($SDL_CFLAGS)
fi

echo "Compiling cimgui..."
OBJECTS=()
for src in "${SOURCES[@]}"; do
    obj="$BUILD_TMP/$(basename "$src" .cpp).o"
    c++ "${CXXFLAGS[@]}" "${INCLUDES[@]}" -c "$src" -o "$obj"
    OBJECTS+=("$obj")
done

echo "Creating static library..."
mkdir -p "$OUT_DIR"
ar rcs "$OUT_DIR/libcimgui.a" "${OBJECTS[@]}"

echo "Done: $OUT_DIR/libcimgui.a"
ls -lh "$OUT_DIR/libcimgui.a"
