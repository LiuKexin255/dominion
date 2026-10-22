#!/usr/bin/env python3
"""出牌结算行为探针：测 Instant Mode 下 act→read 是否可合并为一步。

对每次出牌记录：
- immediate read（POST 返回后零等待首读）的 card_in_play / 手牌数 / 能量
- 结算所需轮询次数与耗时（50ms 间隔）
"""
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import sts2_poc as poc

POLL_INTERVAL = 0.05


def playable_card(st):
    hand = st.get("player", {}).get("hand", [])
    for c in sorted(hand, key=lambda c: c.get("index", 0), reverse=True):
        if c.get("can_play"):
            return c
    return None


def probe(max_plays=12):
    stats = []
    plays = 0
    while plays < max_plays:
        st = poc.get_state()
        t = st.get("state_type")
        if t not in poc.COMBAT_STATES:
            print(f"[probe] 战斗结束: {t}")
            break
        b = st.get("battle", {})
        if not (b.get("turn") == "player" and b.get("is_play_phase")):
            time.sleep(0.5)
            continue
        card = playable_card(st)
        if card is None:
            t0 = time.monotonic()
            poc.post_action({"action": "end_turn"}, "probe_end_turn")
            # end_turn 的 settle：等回到玩家阶段或战斗结束
            while True:
                s = poc.get_state()
                bb = s.get("battle", {})
                if s.get("state_type") not in poc.COMBAT_STATES:
                    break
                if bb.get("turn") == "player" and bb.get("is_play_phase"):
                    break
                time.sleep(POLL_INTERVAL)
            print(f"[probe] end_turn 结算 {time.monotonic()-t0:.2f}s")
            continue

        payload = {"action": "play_card", "card_index": card["index"]}
        if card.get("target_type") == "AnyEnemy":
            tgt = poc.first_alive_enemy(st)
            if tgt:
                payload["target"] = tgt
        hand_before = len(st.get("player", {}).get("hand", []))
        energy_before = st.get("player", {}).get("energy")

        poc.post_action(payload, f"probe_play_{card.get('id','?')}")

        t0 = time.monotonic()
        s1 = poc.get_state()  # 零等待首读
        t_read = time.monotonic() - t0
        b1 = s1.get("battle", {})
        inplay_first = b1.get("card_in_play")
        hand_first = len(s1.get("player", {}).get("hand", []))
        energy_first = s1.get("player", {}).get("energy")

        polls = 0
        while polls < 60:
            if not s1.get("battle", {}).get("card_in_play", False):
                break
            time.sleep(POLL_INTERVAL)
            s1 = poc.get_state()
            polls += 1
        settle_ms = (time.monotonic() - t0) * 1000

        stats.append({
            "card": card.get("name"),
            "inplay_first_read": inplay_first,
            "hand_before": hand_before, "hand_first": hand_first,
            "energy_before": energy_before, "energy_first": energy_first,
            "polls_needed": polls, "settle_ms": round(settle_ms),
            "first_read_ms": round(t_read * 1000),
        })
        print(f"[probe] {card.get('name')}: 首读in_play={inplay_first} "
              f"手牌{hand_before}->{hand_first} 能量{energy_before}->{energy_first} "
              f"轮询{polls}次 结算{settle_ms:.0f}ms")
        plays += 1
    return stats


if __name__ == "__main__":
    stats = probe()
    if stats:
        inplay_seen = sum(1 for s in stats if s["inplay_first_read"])
        consistent = sum(1 for s in stats if s["hand_first"] == s["hand_before"] - 1)
        print(f"\n== 汇总: {len(stats)} 次出牌 ==")
        print(f"首读即结算（in_play=False）: {len(stats)-inplay_seen}/{len(stats)}")
        print(f"首读手牌已扣减: {consistent}/{len(stats)}")
        print(f"平均结算耗时: {sum(s['settle_ms'] for s in stats)/len(stats):.0f}ms")
