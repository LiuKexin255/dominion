#!/usr/bin/env bash
# STS2MCP 自建脚本（Linux dev 侧，游戏机免装 SDK）——【仅作记录，非维护路径】
# 当前策略（2026-10-22 裁定）：不维护 fork，直接使用已打包的 mod/STS2_MCP.dll；
# 本脚本仅在需要重新构建时参考。
# 前置：游戏机 <game>/data_sts2_windows_x86_64/ 下的 sts2.dll / GodotSharp.dll / 0Harmony.dll
#       拷贝到 $REF_DIR；.NET 9 SDK 安装在 $DOTNET_DIR
set -euo pipefail
SRC_DIR="$(cd "$(dirname "$0")" && pwd)/STS2MCP"   # git clone --depth 1 https://github.com/Gennadiyev/STS2MCP.git && git -C STS2MCP checkout 55e0648
REF_DIR="${REF_DIR:-$SRC_DIR/game/data_sts2_windows_x86_64}"
DOTNET_DIR="${DOTNET_DIR:-$HOME/.dotnet}"
export DOTNET_ROOT="$DOTNET_DIR" PATH="$DOTNET_DIR:$PATH"
cd "$SRC_DIR"
git apply "$(dirname "$0")/0001-card-in-play.patch" || echo "(patch 已应用或无需)"
dotnet build STS2_MCP.csproj -c Release -o out/STS2_MCP \
  -p:STS2GameDir="$REF_DIR/.." -p:STS2GameDataDir="$REF_DIR"
echo "产物: $SRC_DIR/out/STS2_MCP/STS2_MCP.dll -> 拷回游戏机 <game>/mods/"
