#!/usr/bin/env python3
"""STS2MCP 真机 PoC 驱动脚本。

执行 survey/sts2-game-control.md §7 清单中 API 驱动的部分（状态采样/开局/战斗
闭环/终局/seed 复现/档位管理），所有请求响应自动留档到同目录 samples/。
仅依赖 Python 标准库；目标地址经 STS2_POC_BASE 环境变量或 --base 覆盖（默认
http://localhost:15526，配合 SSH 隧道或 portproxy 使用，见 README.md §1）。

API 语义依据：https://github.com/Gennadiyev/STS2MCP/blob/main/docs/raw-full.md
（含 seed 仅在 custom/daily 等入口生效——standard 角色选择传 seed 直接报错，
对应 McpMod.Actions.cs 中 Lobby.SetSeed 的调用条件）。
"""

import argparse
import datetime
import json
import os
import pathlib
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

SCRIPT_DIR = pathlib.Path(__file__).resolve().parent
SAMPLES_DIR = SCRIPT_DIR / "samples"
INDEX_PATH = SAMPLES_DIR / "index.jsonl"

COMBAT_STATES = ("monster", "elite", "boss")
HTTP_TIMEOUT = 30  # 状态构建在游戏主线程执行，动画期间可能阻塞数秒，放宽超时


def log(msg: str) -> None:
    ts = datetime.datetime.now().strftime("%H:%M:%S")
    print(f"[{ts}] {msg}", file=sys.stderr)


# ---------------------------------------------------------------- HTTP 层


def http_json(method: str, path: str, body=None, timeout: int = HTTP_TIMEOUT):
    """返回 (status_code, text)；非 2xx 也返回响应体（mod 错误体是 JSON）。"""
    url = base_url() + path
    data = None
    headers = {}
    if body is not None:
        data = json.dumps(body).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.getcode(), resp.read().decode("utf-8", errors="replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", errors="replace")
    except urllib.error.URLError as e:
        raise RuntimeError(
            f"无法连接 {url}: {e.reason}。检查游戏是否运行、mod 是否启用、"
            f"隧道/portproxy 是否建立（README.md §1）。") from e


def base_url() -> str:
    return os.environ.get("STS2_POC_BASE", "http://localhost:15526").rstrip("/")


# ---------------------------------------------------------------- 留档层


def next_seq() -> int:
    SAMPLES_DIR.mkdir(exist_ok=True)
    seqs = [0]
    for p in SAMPLES_DIR.iterdir():
        if p.is_file():
            head = p.name.split("_", 1)[0]
            if head.isdigit():
                seqs.append(int(head))
    return max(seqs) + 1


def archive(label: str, content: str, kind: str = "json", summary: str = "") -> str:
    """所有样本落盘 samples/<seq>_<label>.<kind> 并登记 index.jsonl。"""
    seq = next_seq()
    name = f"{seq:03d}_{label}.{kind}"
    (SAMPLES_DIR / name).write_text(content, encoding="utf-8")
    entry = {
        "seq": seq,
        "ts": datetime.datetime.now().isoformat(timespec="seconds"),
        "label": label,
        "file": name,
        "summary": summary[:200],
    }
    with INDEX_PATH.open("a", encoding="utf-8") as f:
        f.write(json.dumps(entry, ensure_ascii=False) + "\n")
    return name


def archive_json(label: str, obj, summary: str = "") -> str:
    return archive(label, json.dumps(obj, ensure_ascii=False, indent=2), "json", summary)


def archive_text(label: str, text: str, summary: str = "") -> str:
    return archive(label, text, "txt", summary)


# ---------------------------------------------------------------- 状态层


def get_state(fmt: str = "json"):
    code, text = http_json("GET", f"/api/v1/singleplayer?format={fmt}")
    if code != 200:
        raise RuntimeError(f"GET state HTTP {code}: {text[:300]}")
    return json.loads(text) if fmt == "json" else text


def post_action(payload: dict, label: str):
    code, text = http_json("POST", "/api/v1/singleplayer", payload)
    try:
        resp = json.loads(text)
    except json.JSONDecodeError:
        resp = {"raw": text}
    archive_json(f"post_{label}", {"request": payload, "http": code, "response": resp},
                 summary=str(resp.get("message", resp.get("error", ""))))
    if code != 200 or resp.get("status") == "error":
        raise RuntimeError(f"action {payload.get('action')} 失败: {resp}")
    return resp


def summarize(state) -> str:
    if isinstance(state, str):
        return state.splitlines()[0][:120] if state else "(empty)"
    parts = [f"state={state.get('state_type')}"]
    if state.get("menu_screen"):
        parts.append(f"screen={state['menu_screen']}")
        parts.append("options=" + ",".join(menu_options(state)))
    if state.get("run"):
        r = state["run"]
        parts.append(f"act={r.get('act')} floor={r.get('floor')}")
    p = state.get("player")
    if p:
        parts.append(f"{p.get('character')} hp={p.get('hp')}/{p.get('max_hp')} gold={p.get('gold')}")
        if "energy" in p:
            parts.append(f"energy={p.get('energy')}/{p.get('max_energy')} hand={len(p.get('hand', []))}")
    b = state.get("battle")
    if b:
        enemies = ",".join(f"{e.get('name')}({e.get('hp')}hp)" for e in b.get("enemies", []))
        parts.append(f"turn={b.get('turn')} enemies=[{enemies}]")
    return " | ".join(parts)


def menu_options(state) -> list:
    """options 兼容 list[str] 与 list[{name,enabled}] 两种形态（raw-full.md）。"""
    raw = state.get("options", [])
    out = []
    for o in raw:
        out.append(o if isinstance(o, str) else o.get("name", "?"))
    return out


def wait_for(check, timeout: float = 90, interval: float = 1.0, what: str = ""):
    """轮询直到 check(state) 为真；返回命中时的 state。"""
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        last = get_state()
        if check(last):
            return last
        time.sleep(interval)
    raise TimeoutError(f"等待超时({timeout}s): {what or check} 最后状态: {summarize(last)}")


def archive_state_once(label: str, state, seen: set) -> None:
    """每个 state_type 只留档一次，避免轮询刷爆 samples/。"""
    key = state.get("state_type", "?")
    if key not in seen:
        seen.add(key)
        archive_json(label, state, summary=summarize(state))


# ---------------------------------------------------------------- 动作封装


def act_select(option: str, seed: str | None = None):
    payload = {"action": "menu_select", "option": option}
    if seed:
        payload["seed"] = seed
    return post_action(payload, f"select_{option}" + (f"_seeded" if seed else ""))


def act_end_turn():
    return post_action({"action": "end_turn"}, "end_turn")


def first_alive_enemy(state) -> str | None:
    for e in state.get("battle", {}).get("enemies", []):
        if e.get("hp", 0) > 0:
            return e.get("entity_id")
    return None


# ---------------------------------------------------------------- 子命令


def cmd_handshake(args):
    code, text = http_json("GET", "/")
    name = archive_text("handshake", text, summary=text[:120])
    print(f"HTTP {code} -> {name}")
    print(text)
    if code != 200:
        sys.exit(1)


def cmd_state(args):
    st = get_state(args.format)
    if args.archive:
        kind = "json" if args.format == "json" else "txt"
        name = archive(args.label or "state", st if isinstance(st, str)
                       else json.dumps(st, ensure_ascii=False, indent=2), kind,
                       summary=summarize(st))
        print(f"留档 -> {name}")
    if isinstance(st, str):
        print(st)
    else:
        print(summarize(st))
        print(json.dumps(st, ensure_ascii=False, indent=2))


def cmd_sample(args):
    """json + markdown 双格式留档（PoC 清单 #3）。"""
    label = args.label
    st = get_state("json")
    archive_json(label, st, summary=summarize(st))
    md = get_state("markdown")
    archive_text(f"{label}_md", md if isinstance(md, str) else str(md), summary="markdown 视图")
    print(summarize(st))


def cmd_select(args):
    resp = act_select(args.option, args.seed)
    print(json.dumps(resp, ensure_ascii=False))


def navigate_to_character_select(mode: str) -> None:
    """main → singleplayer → [standard|custom 子菜单（若出现）] → character_select。

    v0.107 实测：部分情况下选 singleplayer 后直达 character_select（无模式子菜单），
    两条路径都要兼容。
    """
    st = get_state()
    if st.get("state_type") != "menu":
        raise RuntimeError(f"当前不在菜单: {summarize(st)}")
    screen = st.get("menu_screen", "main")
    if screen == "main":
        act_select("singleplayer")
        st = wait_for(lambda s: s.get("menu_screen") in ("singleplayer", "character_select"),
                      what="singleplayer 子菜单或 character_select")
    screen = st.get("menu_screen")
    if screen == "singleplayer":
        if mode not in menu_options(st):
            raise RuntimeError(f"singleplayer 子菜单无 {mode}: {menu_options(st)}")
        act_select(mode)
        wait_for(lambda s: s.get("menu_screen") == "character_select", what="character_select")
    elif screen != "character_select":
        raise RuntimeError(f"意外的菜单层: {screen}, options={menu_options(st)}")


def cmd_start(args):
    """开局封装：标准局不带 seed；seed 局走 custom 入口（standard 传 seed 会被拒）。"""
    mode = "custom" if args.seed else "standard"
    navigate_to_character_select(mode)
    st = get_state()
    options = menu_options(st)
    char = args.character
    match = next((o for o in options if o.upper() == char.upper()), None)
    if not match:
        raise RuntimeError(f"角色 {char} 不在可选列表: {options}")
    log(f"选择角色 {match}（mode={mode}{' seed=' + args.seed if args.seed else ''}）")
    # 角色选择与出发是两步：先选角色，再 confirm/embark；seed 随 confirm 生效
    # （McpMod.Actions.cs ExecuteCharacterSelectMenuOption 的 seed 处理在 confirm/embark 分支）
    act_select(match)
    act_select("confirm", args.seed)
    first = wait_for(lambda s: s.get("state_type") != "menu", timeout=120, what="开局离开菜单")
    archive_json("start_first_state", first, summary=summarize(first))
    print(summarize(first))


def wait_card_settled(timeout: float = 10) -> None:
    """等待出牌动画结算（battle.card_in_play 变 False）再出下一张。

    依赖 mod 补丁暴露的 card_in_play 字段（游戏自身的 end_turn 守卫信号
    NPlayerHand.InCardPlay）；旧版 mod 无此字段时视为恒已结算，退化为旧行为。
    """
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        st = get_state()
        if st.get("state_type") not in COMBAT_STATES:
            return
        if not st.get("battle", {}).get("card_in_play", False):
            time.sleep(0.3)  # 结算后的短暂静默，避免压到下一动画首帧
            return
        time.sleep(0.3)


def play_one_card(state) -> bool:
    """从高索引往低出可打的第一张牌（打出后手牌索引左移，重查状态再选下一张）。

    出牌与状态读取之间存在动画竞态（手牌数在请求飞行期间变化），失败返回
    False 交由调用方短暂等待后重查重试，而非直接中止。
    """
    hand = state.get("player", {}).get("hand", [])
    for card in sorted(hand, key=lambda c: c.get("index", 0), reverse=True):
        if not card.get("can_play", False):
            continue
        payload = {"action": "play_card", "card_index": card["index"]}
        if card.get("target_type") == "AnyEnemy":
            target = first_alive_enemy(state)
            if not target:
                continue
            payload["target"] = target
        try:
            post_action(payload, f"play_{card.get('id', card['index'])}")
            return True
        except RuntimeError as e:
            log(f"出牌失败将重试: {e}")
            return False
    return False


def wait_player_phase(timeout: float = 60):
    return wait_for(
        lambda s: s.get("state_type") not in COMBAT_STATES
        or (s.get("battle", {}).get("turn") == "player" and s.get("battle", {}).get("is_play_phase")),
        timeout=timeout, what="玩家阶段或战斗结束")


def cmd_combat(args):
    """战斗闭环：auto 出牌 → end_turn → 轮询敌回合，直至离开战斗状态。"""
    start_ts = time.monotonic()
    seen: set = set()
    turns = 0
    while turns < args.max_turns:
        st = get_state()
        t = st.get("state_type")
        if t in COMBAT_STATES:
            archive_state_once("combat_state", st, seen)
            b = st.get("battle", {})
            if b.get("turn") == "player" and b.get("is_play_phase"):
                if not args.passive:
                    failures = 0
                    while True:
                        cur = get_state()
                        if cur.get("state_type") not in COMBAT_STATES:
                            break
                        if not (cur.get("battle", {}).get("turn") == "player"
                                and cur.get("battle", {}).get("is_play_phase")):
                            break
                        if play_one_card(cur):
                            failures = 0
                            wait_card_settled()
                        else:
                            failures += 1
                            if failures >= 3:
                                break
                            time.sleep(1.0)
                try:
                    act_end_turn()
                except RuntimeError as e:
                    # 我方出牌致敌全灭时战斗即结束，end_turn 落空是正常竞态
                    if "Not in combat" not in str(e):
                        raise
                turns += 1
                log(f"第 {turns} 回合结束，等待敌方行动…")
                wait_player_phase()
            else:
                time.sleep(1.0)
        elif t == "hand_select":
            archive_state_once("hand_select", st, seen)
            post_action({"action": "combat_select_card", "card_index": 0}, "hand_select_0")
            post_action({"action": "combat_confirm_selection"}, "hand_select_confirm")
        else:
            break
    elapsed = time.monotonic() - start_ts
    final = get_state()
    print(f"战斗结束（{turns} 回合，{elapsed:.0f}s）-> {summarize(final)}")
    if args.passive and final.get("state_type") in COMBAT_STATES:
        sys.exit(2)  # passive 模式预期死亡离开战斗，仍在战斗说明回合数不够


def cmd_to_menu(args):
    st = get_state()
    t = st.get("state_type")
    if t == "menu":
        print("已在主菜单")
        return
    if t != "game_over":
        raise RuntimeError(f"仅支持 game_over → main_menu（当前: {summarize(st)}）；"
                           "run 进行中无 API 弃局，需先打完/死亡")
    act_select("main_menu")
    st = wait_for(lambda s: s.get("menu_screen") == "main", what="主菜单")
    print(summarize(st))


def autopilot_until_game_over(max_steps: int = 200) -> None:
    """seed-check 用的速通驾驶员：目标是最快死亡回到 game_over。

    遇到未知/overlay 状态即中止（留档已写入），由人工介入后重跑。
    """
    seen: set = set()
    for _ in range(max_steps):
        st = get_state()
        t = st.get("state_type")
        archive_state_once("autopilot", st, seen)
        if t == "game_over":
            return
        if t in COMBAT_STATES:
            b = st.get("battle", {})
            if b.get("turn") == "player" and b.get("is_play_phase"):
                act_end_turn()
            else:
                time.sleep(1.0)
        elif t == "menu":
            raise RuntimeError("autopilot 意外回到菜单")
        elif t == "hand_select":
            post_action({"action": "combat_select_card", "card_index": 0}, "ap_hand_0")
            post_action({"action": "combat_confirm_selection"}, "ap_hand_confirm")
        elif t == "event":
            ev = st.get("event", {})
            if ev.get("in_dialogue"):
                post_action({"action": "advance_dialogue"}, "ap_dialogue")
            else:
                post_action({"action": "choose_event_option", "index": 0}, "ap_event_0")
        elif t == "card_reward":
            post_action({"action": "select_card_reward", "card_index": 0}, "ap_cardreward_0")
        elif t == "card_select":
            post_action({"action": "select_card", "index": 0}, "ap_cardselect_0")
            cs = st.get("card_select", {})
            if cs.get("can_confirm"):
                post_action({"action": "confirm_selection"}, "ap_cardselect_confirm")
        elif t == "bundle_select":
            post_action({"action": "select_bundle", "index": 0}, "ap_bundle_0")
            post_action({"action": "confirm_bundle_selection"}, "ap_bundle_confirm")
        elif t == "relic_select":
            post_action({"action": "select_relic", "index": 0}, "ap_relic_0")
        elif t == "treasure":
            post_action({"action": "proceed"}, "ap_treasure_proceed")
        elif t in ("rewards", "rest_site", "shop", "fake_merchant"):
            post_action({"action": "proceed"}, f"ap_{t}_proceed")
        elif t == "map":
            opts = st.get("map", {}).get("next_options", [])
            idx = 0
            monster = next((o for o in opts if o.get("type") in ("Monster", "Elite")), None)
            if monster is not None:
                idx = monster.get("index", 0)
            post_action({"action": "choose_map_node", "index": idx}, f"ap_map_{idx}")
        elif t == "crystal_sphere":
            if st.get("crystal_sphere", {}).get("can_proceed"):
                post_action({"action": "crystal_sphere_proceed"}, "ap_cs_proceed")
            else:
                cells = st.get("crystal_sphere", {}).get("clickable_cells", [])
                if cells:
                    c = cells[0]
                    post_action({"action": "crystal_sphere_click_cell", "x": c["x"], "y": c["y"]},
                                "ap_cs_click")
                else:
                    time.sleep(1.0)
        else:
            raise RuntimeError(f"autopilot 遇到不认识的状态，需人工介入: {summarize(st)}")
        time.sleep(1.0)
    raise RuntimeError(f"autopilot 超过 {max_steps} 步未到 game_over")


def run_seeded_once(seed: str, run_no: int) -> dict:
    log(f"—— 第 {run_no} 局（seed={seed}）——")
    st = get_state()
    if st.get("state_type") != "menu":
        raise RuntimeError(f"需先回主菜单（当前: {summarize(st)}）")
    cmd_start(argparse.Namespace(seed=seed, character="IRONCLAD"))
    first = wait_for(lambda s: s.get("state_type") != "menu", timeout=120, what="开局")
    snapshot = json.loads(json.dumps(first))  # 深拷贝固定首态
    archive_json(f"seedrun{run_no}_first", snapshot, summary=summarize(snapshot))
    autopilot_until_game_over()
    cmd_to_menu(argparse.Namespace())
    return snapshot


def cmd_seed_check(args):
    """同 seed 两局对比首态确定性（PoC 清单 #8）。"""
    snaps = [run_seeded_once(args.seed, i + 1) for i in range(2)]
    same = snaps[0] == snaps[1]
    # 对比结论单独留档，含两局首态摘要
    archive_json("seed_check_verdict", {
        "seed": args.seed,
        "deterministic_first_state": same,
        "run1": summarize(snaps[0]),
        "run2": summarize(snaps[1]),
    }, summary=f"first_state_equal={same}")
    print(f"同 seed 两局首态{'一致' if same else '不一致'}（详见 samples/seed_check_verdict 与 seedrun*_first）")


def cmd_profiles(args):
    code, text = http_json("GET", "/api/v1/profiles")
    data = json.loads(text)
    archive_json("profiles", data)
    print(json.dumps(data, ensure_ascii=False, indent=2))


def cmd_profile_switch(args):
    code, text = http_json("POST", "/api/v1/profiles",
                           {"action": "switch", "profile_id": args.id})
    archive_json(f"profile_switch_{args.id}", {"http": code, "response": json.loads(text)})
    print(text)


def cmd_profile_delete(args):
    code, text = http_json("POST", "/api/v1/profiles",
                           {"action": "delete", "profile_id": args.id})
    archive_json(f"profile_delete_{args.id}", {"http": code, "response": json.loads(text)})
    print(text)


def cmd_wiki(args):
    q = urllib.parse.urlencode({"query": args.query, "item_type": args.type, "limit": args.limit})
    code, text = http_json("GET", f"/api/v1/wiki?{q}")
    data = json.loads(text)
    archive_json("wiki", data)
    for r in data.get("results", []):
        print(f"[{r.get('item_type')}] {r.get('name')} (score={r.get('score')})")
    if not data.get("results"):
        print(json.dumps(data, ensure_ascii=False, indent=2))


def cmd_compendium(args):
    code, text = http_json("GET", "/api/v1/compendium")
    data = json.loads(text)
    archive_json("compendium", data)
    sections = data.get("sections", {})
    print(f"profile={data.get('profile_id')} sections={list(sections)}")
    print(json.dumps(data, ensure_ascii=False, indent=2)[:2000])


def cmd_get(args):
    code, text = http_json("GET", args.path)
    archive_text(f"get_{args.path.strip('/').replace('/', '_')}", text, summary=f"HTTP {code}")
    print(f"HTTP {code}")
    print(text)
    if code >= 400:
        sys.exit(1)


def cmd_post(args):
    body = json.loads(args.body)
    code, text = http_json("POST", args.path, body)
    archive_json(f"post_{args.path.strip('/').replace('/', '_')}",
                 {"request": body, "http": code, "response": json.loads(text)})
    print(f"HTTP {code}")
    print(text)
    if code >= 400:
        sys.exit(1)


# ---------------------------------------------------------------- 入口


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--base", default=None, help="mod API 基址（默认 $STS2_POC_BASE 或 http://localhost:15526）")
    sub = parser.add_subparsers(dest="cmd", required=True)

    sub.add_parser("handshake", help="GET / 握手验证（清单 #2）")

    p = sub.add_parser("state", help="读取单人状态")
    p.add_argument("--format", choices=["json", "markdown"], default="json")
    p.add_argument("--label", default=None)
    p.add_argument("--archive", action="store_true", help="留档到 samples/")

    p = sub.add_parser("sample", help="json+markdown 双格式采样留档（清单 #3）")
    p.add_argument("--label", required=True, help="场景标签，如 menu/combat/map/shop/event")

    p = sub.add_parser("select", help="原子 menu_select")
    p.add_argument("--option", required=True)
    p.add_argument("--seed", default=None)

    p = sub.add_parser("start", help="开局（清单 #4）：无 seed 走 standard；有 seed 走 custom")
    p.add_argument("--seed", default=None)
    p.add_argument("--character", default="IRONCLAD")

    p = sub.add_parser("combat", help="战斗闭环（清单 #5）")
    p.add_argument("--max-turns", type=int, default=30)
    p.add_argument("--passive", action="store_true", help="只 end_turn 不出牌（速死/清单 #8）")

    sub.add_parser("to-menu", help="game_over → 主菜单（清单 #7）")

    p = sub.add_parser("seed-check", help="同 seed 两局确定性对比（清单 #8）")
    p.add_argument("--seed", required=True)

    sub.add_parser("profiles", help="列出 profile 档位（清单 #9）")
    p = sub.add_parser("compendium", help="读取 compendium（含 run 历史）")
    p = sub.add_parser("profile", help="读取当前 profile 进度")

    p = sub.add_parser("profile-switch", help="切换档位（清单 #9）")
    p.add_argument("--id", type=int, required=True, choices=[1, 2, 3])
    p = sub.add_parser("profile-delete", help="删除非活跃档位（清单 #9）")
    p.add_argument("--id", type=int, required=True, choices=[1, 2, 3])

    p = sub.add_parser("wiki", help="已发现卡牌/遗物模糊搜索")
    p.add_argument("--query", required=True)
    p.add_argument("--type", choices=["all", "card", "relic"], default="all")
    p.add_argument("--limit", type=int, default=10)

    p = sub.add_parser("get", help="万能 GET（探索兜底）")
    p.add_argument("path")
    p = sub.add_parser("post", help="万能 POST（探索兜底，body 为 JSON 字符串）")
    p.add_argument("path")
    p.add_argument("body")

    args = parser.parse_args()
    if args.base:
        os.environ["STS2_POC_BASE"] = args.base

    handlers = {
        "handshake": cmd_handshake, "state": cmd_state, "sample": cmd_sample,
        "select": cmd_select, "start": cmd_start, "combat": cmd_combat,
        "to-menu": cmd_to_menu, "seed-check": cmd_seed_check,
        "profiles": cmd_profiles, "compendium": cmd_compendium,
        "profile-switch": cmd_profile_switch, "profile-delete": cmd_profile_delete,
        "wiki": cmd_wiki, "get": cmd_get, "post": cmd_post,
    }
    if args.cmd == "profile":
        args.path = "/api/v1/profile"
        cmd_get(args)
        return
    handlers[args.cmd](args)


if __name__ == "__main__":
    main()
