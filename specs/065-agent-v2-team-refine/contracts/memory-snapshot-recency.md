# Contract: planner 长期记忆快照——memory 服务 ListMemories 通用排序与游标分页

**Feature**: specs/065-agent-v2-team-refine/spec.md（FR-007，US3）
**服务端锚点**: `projects/game/game.proto`（ListMemoriesRequest）、`projects/game/memory/{domain,handler,runtime/mongo}`
**客户端锚点**: `common/js/dsh-plugins/memory-service/src/{client,snapshot,service}.ts`
**基线契约**: `specs/064-memory-split-fold-remain/contracts/dsh-plugins.md` §2（快照固定与 section——注入策略增量修订，冻结时机与写路径零改动）；`specs/039-planner-memory-calibration/contracts/memory-service-contract.md` §2（ListMemories RPC 基线——本契约定义 `order_by` 面）。

## §1 ListMemories 通用排序接口（memory 服务端）

排序是一套**通用机制**：`order_by` 语法解析 → 字段白名单映射 → 指定 tie-breaker 收尾 → 通用游标 → 单路径仓储查询。不为任何单个排序需求定制方法、字面量或游标形态，也不按排序键数量分支。**全部校验收敛在 domain 层，仓储收到的一律是已验证输入、只做直译**；实现形态以 Go 语言习惯为准（2026-09-16 第三、四、五次用户裁定，裁定依据与被否决形态见 research.md D4）。**游标值的全生命周期（wire string ↔ typed ↔ 文档值）责任单一、成套、对称**：`string ↔ typed` 往返收敛在 domain 一处，仓储只搬运 typed 值（对象关系与值转换责任总图见 item 9）。

1. **`order_by` 语法**（AIP-132 Ordering: https://google.aip.dev/132）：
   - 值为逗号分隔的字段列表，每项为 `{field}` 或 `{field} desc`；升序通过**省略**后缀表达（AIP-132 只定义 `desc` 后缀，`asc` 不是合法 token）；空白不敏感（项内与项间任意空白等价）；`desc` 小写敏感。
   - **字段白名单**（API 字段 → Mongo 字段的单一事实源映射表，domain 包内私有 `[]*memorySortFieldSpec{Field, MongoField, kind}`——行结构不含唯一性标志）：`memory_id`（string）、`update_time`（time）——首期集合从实际消费出发，机制不为该集合写死。白名单字段在键列表中**任意位置合法**（含首位/中间位/末位）。
   - 非法值 → `INVALID_ARGUMENT`（AIP-193: https://google.aip.dev/193），错误信息列受支持字段与语法：未知字段（含子字段路径，如 `content.foo`）、非法后缀（`asc`、`DESC`、多余 token）、重复字段。
2. **指定 tie-breaker 收尾规则**：请求排序字段**完全不包含 `memory_id`**（指定的唯一键字段）时，自动追加 `memory_id asc`——排序成为确定全序，游标才能稳定翻页（并列键不以唯一键收尾会在页边界静默跳行/重行，见 research.md R0 社区论证；Firestore Standard edition 对 `orderBy` 缺省自动追加 `__name__` 是同一规则的工业先例——其追加同样只发生在键列表**不含** `__name__` 时）。**判据依据**：排序键元组逐文档唯一 ⇔ 全序；`memory_id` 在 (template, session) scope 内唯一，故只要它出现在**任意键位**（不必末位），元组即逐文档唯一——无需也不应再填补。tie-breaker 是**一个指定字段**而非"扫描唯一字段"的通用规则——保证全序的唯一字段有一个就够，即使将来白名单出现其他唯一字段也不参与收尾。**缺省（空 `order_by`）早返回固定默认值** `[]*MemorySortTerm{memory_id asc}`（每次调用新构造——返回切片可被调用方持有，不共享包级单例）；语义与"空键列表 + 填补规则"一致，无缺省特设分支。
3. **键数无上界、构造无长度分支**：最终排序键的键数随白名单业务字段数增长（N 个业务字段至多产出 N+1 键），机制不为任何特定键数写死。仓储 seek 条件（item 6）对**任意键数**以同一 OR 阶梯循环统一生成——单键自然退化为一个子句的 `$or`（与裸比较语义等价，不做"并入 filter"特例）。新增白名单业务字段时 seek 构造**零改动**（仅剩 item 7 的 accessor 与索引义务）。
4. **通用游标**（AIP-158 Pagination: https://google.aip.dev/158）：
   - **struct 包装的单一 wire 形态 + 指针签名**：`type MemoryPageCursor struct { Entries []*MemoryCursorEntry \`json:"entries"\` }`、`MemoryCursorEntry{Field string \`json:"field"\`, Value string \`json:"value"\`}`——字段一律 `string`（无指针字段、无中间 JSON 结构体与转换层）；容器元素 `[]*MemoryCursorEntry`（`style/golang.md` 指针节）。**跨函数边界一律 `*MemoryPageCursor`**（`style/golang.md` 函数参数与返回值优先指针）；**nil = 首页的唯一判据**（handler 空 token 传 nil；仓储 `cursor != nil` 才构造 seek）。非 nil cursor 携带**非空且 typed 就绪**的 Entries——这是 `DecodeMemoryPageToken` 产物的入参契约，手构空 Entries 的非 nil cursor 属违约输入（fail-fast，同 `memoryDocument.sortValue` 未知字段 panic 先例），不存在"手构空 Entries = 首页"的第二判据。**演进性**：未来向 token 添加参数（如版本、过滤条件回显）= struct 加一个带 json tag 的字段，`Encode`/`Decode`/handler/仓储签名与全部调用点不变——struct 是 token 形态的唯一演进点（裸 slice 类型无此扩展位）。
   - **entry 的 wire/typed 双形态**：`MemoryCursorEntry` 另有包内私有 `typed any` 字段——decode/构造时**一次性**转换的 typed 值（string 原样 / `time.Time`），`encoding/json` 对未导出字段不可见，wire 形态仍为 `{field, value}` 全 string；导出方法 `Typed() any` 供仓储直读。**每个游标值在其生命周期内恰转换一次**（见 item 9 责任表）。
   - **不编码方向**：token 是结果空间中的**位置**；方向由续页请求自身的 `order_by` 重新推导（Firestore `startAfter` 语义：游标 = 排序键值位置，序由查询定义）。
   - **编码是全函数**：`EncodeMemoryPageToken(cursor *MemoryPageCursor) string` 不返回 error（全 string 结构无失败路径）；nil cursor 或空 Entries → 空 token。
   - **解码 + 键匹配 + 类型校验 + 转换在 domain 一步完成**：`DecodeMemoryPageToken(token string, sort []*MemorySortTerm) (*MemoryPageCursor, error)`（成功恒返回非 nil 指针），以下情形全部 wrap `ErrInvalidPageToken` → `INVALID_ARGUMENT`：空 token、坏 base64、坏 JSON（含顶层非对象——如顶层数组形态）、`entries` 缺失/非数组/为空、元素为 null、元素缺 `field`/`value`（空串）、字段序列与请求最终排序键不匹配（长度或字段名——涵盖未知 field 与**跨序重放**，如 `update_time desc` 的 token 配缺省序请求）、值与字段声明类型不符（时间不可解析 RFC3339Nano）。逐 entry 经 `sort[i].CursorValue(entry.Value)` **一次完成校验与转换**——typed 值存入 entry，仓储 thereafter 不再转换。等价拼写的 `order_by`（如 `update_time desc` 与 `update_time desc, memory_id`，最终排序键相同）token 互通。token 不透明（AIP-158 Opacity），客户端只透传。
   - **wire 兼容性**：token 对外是 opaque 字符串（AIP-158 Opacity），内部 JSON 形态为对象 `{"entries":[...]}`。全消费面（JS 客户端透传、写路径单次调用内翻页、testplan 断言）只透传不解析，且无跨版本持久 token 消费面（写路径翻页循环在单次调用内完成、测试每次从首页开始）——token 内部形态变化零破坏。`order_by` 对外语义面仅一处变化：曾因"`memory_id` 非末位"被拒绝的键列表（如 `memory_id, update_time desc`）转为**合法**（第五轮裁定修正——合法集合扩大；消费面无依赖：JS 客户端只发送 `update_time desc` 或缺省）。
5. **分层**（校验前置于 domain，仓储直译；`style/golang.md` 函数节——已校验入参不重复校验）：
   - `domain`：
       - `ParseMemoryOrderBy(orderBy string) ([]*MemorySortTerm, error)`——**空白早返回固定默认值，主流程保持在顶层**；非空输入**两趟**处理，即"先校验后产出"的字面形态（校验趟完成全部校验后才进入产出趟，第一趟无外部可见产出）：**校验趟**逐项校验（语法 → 白名单 → 重复）并将构造出的 `spec.term(descending)` **存入 map**（field → `*MemorySortTerm`）；任一项失败即 `(nil, error)`（无部分产出——map 为函数内局部状态，不随错误泄漏）。**排序趟再过一遍 items** 按请求原序从 map 取 term 组装切片——map 迭代序随机（https://go.dev/blog/maps "Iteration order"），请求原序由**重放输入**恢复：`items` 是 `strings.Split` 的输入产物而非任何派生结构，与第三轮否决的**跨函数签名随行**派生 specs 平行数组性质不同，无平行数组回归；排序趟直接取用 map 中的 term，无 spec→term 二次构造。循环后 map 无 `memory_id` 才追加 tie-breaker（填补项不在 items 中，置于输出末尾）。`validated := make(map[string]*MemorySortTerm)` 是**唯一已验证存储**，一个 map 兼三职：重复检查（key 存在）、`memory_id` 存在性查找（填补判据）、term 的直接存放与取用。终态骨架：

        ```
        ParseMemoryOrderBy(orderBy string) ([]*MemorySortTerm, error):
            if strings.TrimSpace(orderBy) == "":                      // 空白早返回（空串/纯空白同归一路径）
                return []*MemorySortTerm{memorySortTieBreaker.term(false)}, nil
            items := strings.Split(orderBy, ",")
            validated := make(map[string]*MemorySortTerm)             // 唯一已验证存储：field → term 本体
            for _, item := range items:                               // 第一趟（校验趟）：逐项校验 + 构造入 map
                fields := strings.Fields(item)                        // 项内空白归一
                if len(fields) ∉ {1,2} 或 (len==2 且 fields[1] != "desc"):
                    return nil, 语法错误（列受支持语法与字段）          // map 为局部状态——失败无部分产出泄漏
                spec, ok := memorySortFieldSpecByField(fields[0])
                if !ok: return nil, 未知字段错误
                if _, dup := validated[spec.Field]; dup:
                    return nil, 重复字段错误
                validated[spec.Field] = spec.term(len(fields) == 2)
            var terms []*MemorySortTerm
            for _, item := range items:                               // 第二趟（排序趟）：map 迭代序随机——
                terms = append(terms, validated[strings.Fields(item)[0]])  // 再过一遍 items 恢复请求原序（第一趟已保证在 map 中）
            if _, ok := validated[memorySortTieBreaker.Field]; !ok:   // 填补判据：map 完全不含 memory_id
                terms = append(terms, memorySortTieBreaker.term(false))    // 不在 items 中，置于输出末尾
            return terms, nil
        ```

      - `MemorySortTerm{Field, MongoField string; Descending bool}`（另携带包内私有的字段类型 `kind`——kind 常量以 string 为零值，iota 首位，测试手构 term（零 kind）即 string 语义，无需包内构造器）——**自包含的最终键元素**：仓储直译 sort/filter 所需的字段映射与方向全部随 term 携行，仓储不回头调用 domain 查表，无派生平行数组。**term 同时是游标值转换的唯一知识源**，携带一对**对称**转换方法：`CursorValue(value string) (any, error)`（wire string → typed：string 原样、time `time.Parse(time.RFC3339Nano)`）与 `CursorEntry(value any) *MemoryCursorEntry`（typed → entry：string 原样、time `UTC().Format(time.RFC3339Nano)` 格式化——typed → wire 的唯一所在；kind 与 value 动态类型不匹配 panic fail-fast——编程期不变量违约，同 `memoryDocument.sortValue` 未知字段先例，无 error 路径）。
      - 白名单映射表（包内私有单一事实源，行结构见 item 1）。
      - `MemoryCursorEntry`（wire 结构 + 私有 `typed` + `Typed() any`）/ `MemoryPageCursor`（struct 包装）/ `EncodeMemoryPageToken`（全函数，指针入参）/ `DecodeMemoryPageToken`（解码 + 匹配 + 校验 + 转换，指针返回）/ `ErrInvalidPageToken`。
   - `handler`：字符串透传与错误映射——`order_by` 解析错误 → `INVALID_ARGUMENT`；`ErrInvalidPageToken`（含解码错误）→ `INVALID_ARGUMENT`；**空 token 时 `cursor` 保持 nil 传仓储**（`var cursor *domain.MemoryPageCursor`）；无排序知识：

        ```
        sort, err := domain.ParseMemoryOrderBy(req.GetOrderBy())     // 错误 → INVALID_ARGUMENT
        var cursor *domain.MemoryPageCursor                          // nil = 首页
        if token := req.GetPageToken(); token != "":
            cursor, err = domain.DecodeMemoryPageToken(token, sort)  // 错误 → INVALID_ARGUMENT
        repo.ListMemories(ctx, template, session, sort, cursor, pageSize)
        ```

   - `runtime/mongo`：`ListMemories(ctx, template, session, sort []*domain.MemorySortTerm, cursor *domain.MemoryPageCursor, pageSize int)` 一套流程无 per-order 方法：sort → Mongo sort 文档（`MongoField` × 方向直译）；`cursor != nil`（nil 即首页）→ seek 条件（item 6）；`SetLimit(pageSize + 1)`（既有 limit+1 模式）；页满按最终键从页末条目构造 entries（`term.CursorEntry(doc.sortValue(term.Field))`——typed 直入，格式化在 term）→ `EncodeMemoryPageToken(&cursor)`。**仓储全程零 string↔typed 转换**。
6. **seek 条件（通用 OR 阶梯，无长度分支，O(n) 增量构造）**：对最终排序键的**任意键数**以单一循环统一生成——行值比较 `(k₁..kₙ) 排在游标之后` 的标准 OR 阶梯展开（seek method：https://use-the-index-luke.com/sql/partial-results/fetch-next-page；Mongo `$or` 标准形：https://brianp.de/posts/2024/mongodb-cursor-pagination-multiple-fields/——见 research.md R0）。构造为包内私有函数，**无 error 返回**（构造路径不含任何可失败转换——typed 值已由 decode 一次产出）：

   ```
   memorySeekClauses(sort []*MemorySortTerm, cursor *MemoryPageCursor) bson.A:
       clauses := bson.A{}
       prefix := bson.M{}                                // 等值前缀（keys 0..i-1）——逐键演进的派生基
       for i, term := range sort:
           value := cursor.Entries[i].Typed()            // typed 值：decode 已一次转换，此处零转换
           clause := 浅拷贝(prefix)                        // 独立子句文档（maps.Copy 或等价循环）
           clause[term.MongoField] = bson.M{memoryCmpOp(term.Descending): value}
           clauses = append(clauses, clause)             // 第 i 子句 = 前缀等值 + 当前键方向比较
           prefix[term.MongoField] = value               // 尾项退化为等值、汇入前缀（O(1)）——第 i+1 子句由此派生
       return clauses
   ```

   **增量构造与摊还分析**：前序条件不从零重建——每个键位的等值化恰发生一次（`prefix[kᵢ] = vᵢ`，O(1)）；第 i 子句 = 前缀的**浅拷贝**（i 项）+ 尾项 O(1)。子句是彼此独立的 `bson.M`（mongo driver 序列化要求顶层 map 不共享；浅拷贝足够——entry 值均为 string/`time.Time` 不可变标量，跨子句共享安全）。总工作量 Θ(子句总项数) = 输出规模（n 子句共 n(n+1)/2 项——OR 阶梯输出本身即 Θ(n²) 规模，是输出的下界），**每条输出恰写一次**：零重复类型转换（转换零次——decode 已完成）、零重复白名单查找、零重复前缀构建——"O(n)" 的准确含义是**每键摊还 O(1) 的状态推进 + 总工作量输出敏感**。`ListMemories` 将结果统一赋给 `filter["$or"]`（与 scope filter 并列）。**单键自然退化为一个子句的 `$or`**（`$or: [{memory_id: {$gt: m}}]` 与裸比较 `{memory_id: {$gt: m}}` 语义等价）——不做"单键并入 filter"特例，不按 `len(sort)` 分支。入参仅 `sort` 与 `cursor` 两个已验证对象（`MemorySortTerm` 自包含 MongoField/kind，entry 携带 typed），无 specs/sort/values 平行数组随行。两键形态 `$or: [{k₁: {op₁}}, {k₁: v₁, k₂: {op₂}}]` 是该循环在键长 2 下的产出——session 服务 `List` 的字面两子句（`projects/game/session/runtime/mongo/repository.go`）即通用阶梯在固定双键服务中的手写特例。
7. **索引**：唯一索引 `(template, session_id, memory_id)` 支撑缺省 `[memory_id asc]`（等值前缀 + 末键排序）；非唯一复合索引 `{template: 1, session_id: 1, update_time: -1, memory_id: 1}`（既有）支撑 `[update_time desc, memory_id asc]`（键序与方向精确匹配，ESR：等值前缀在前、排序键随后——https://www.mongodb.com/docs/manual/tutorial/equality-sort-range-guideline/）。**索引义务约定**：新增一个可排序字段 = 白名单加一行 + Mongo 文档 accessor 一行 + 按实际消费方向建 `(template, session_id, <field>[, memory_id])` 复合索引。未消费方向的组合（如 `update_time asc`）语法合法，由既有索引扫描 + 内存排序兜底（复合索引只支撑同序与逆序 sort：https://www.mongodb.com/docs/v7.0/tutorial/sort-results-with-indexes/）；记忆条目典型 <100/会话，规模下无碍；出现实际消费方向时再补建对应索引。
8. **HTTP 面**：grpc-gateway GET 的非路径字段自动映射查询参数（同 `page_size`/`page_token` 机制），`order_by` 无需额外注解。
9. **对象关系与值转换责任（classDiagram）**——handler/domain/mongo 各对象：谁创建谁、谁持有谁、值转换责任在谁、调用方向、nil 语义：

   ```mermaid
   classDiagram
       class Handler {
           <<handler 层：字符串透传与错误映射，无排序知识>>
           +ListMemories(req) resp
       }
       class MemoryRepository {
           <<domain 接口>>
           +ListMemories(ctx, template, session, sort, cursor *MemoryPageCursor, pageSize) ([]*Memory, string, error)
       }
       class memorySort {
           <<domain 包内排序知识>>
           +ParseMemoryOrderBy(orderBy string) ([]*MemorySortTerm, error)
           -memorySortFieldSpecs []*memorySortFieldSpec
       }
       class memorySortFieldSpec {
           <<domain 白名单行（单一事实源）>>
           +Field string
           +MongoField string
           -kind memorySortValueKind
       }
       class MemorySortTerm {
           <<domain 自包含最终键元素>>
           +Field string
           +MongoField string
           +Descending bool
           -kind memorySortValueKind
           +CursorValue(wire string) (any, error)
           +CursorEntry(value any) *MemoryCursorEntry
       }
       class MemoryCursorCodec {
           <<domain 包内函数>>
           +EncodeMemoryPageToken(cursor *MemoryPageCursor) string
           +DecodeMemoryPageToken(token string, sort []*MemorySortTerm) (*MemoryPageCursor, error)
       }
       class MemoryPageCursor {
           <<domain：nil = 首页（唯一判据）>>
           +Entries []*MemoryCursorEntry
       }
       class MemoryCursorEntry {
           <<domain：wire 结构 + typed 双形态>>
           +Field string
           +Value string
           -typed any
           +Typed() any
       }
       class memoryRepository {
           <<runtime/mongo 实现：已验证输入直译，零转换>>
       }
       class memoryDocument {
           <<runtime/mongo 文档模型>>
           +sortValue(field string) any
       }

       Handler --> MemoryRepository : 调用（sort + cursor，空 token 时 cursor = nil）
       Handler ..> memorySort : order_by 字符串 → 最终键
       Handler ..> MemoryCursorCodec : page token → *cursor（空 token 不调用）
       memorySort ..> memorySortFieldSpec : 白名单校验 + 填补判据（map 存在性）
        memorySort ..> MemorySortTerm : 校验趟构造并存入 validated map（排序趟按 items 原序取用；map 无 memory_id 时追加 tie-breaker）
       MemoryCursorCodec ..> MemorySortTerm : Decode 经 CursorValue 校验+转换（每 entry 恰一次，wire→typed）
       MemoryCursorCodec ..> MemoryPageCursor : Decode 创建 *cursor（成功恒非 nil）；Encode 序列化（typed→wire 不在此处——见 CursorEntry）
       MemoryPageCursor *-- MemoryCursorEntry : Entries（容器元素指针）
       MemorySortTerm ..> MemoryCursorEntry : CursorEntry 创建（typed→wire 唯一所在，next-token 方向）
       MemoryRepository <|.. memoryRepository : 实现
       memoryRepository ..> MemorySortTerm : 读 MongoField/Descending/CursorEntry（直译）
       memoryRepository ..> MemoryCursorEntry : 读 Typed()（typed 原样入 bson）
       memoryRepository ..> memoryDocument : 解码文档 / sortValue(field) 取 typed
       memoryRepository ..> MemoryCursorCodec : 页末 &cursor → next token
       memoryDocument ..> MemorySortTerm : sortValue 按 term.Field 返回 typed（无格式化）
   ```

   **值转换责任表**（每条形态跳跃唯一归属层、唯一发生点、恰一次）：

   | 值形态跳跃 | 方向 | 责任层 · 唯一发生点 | 次数 |
   |---|---|---|---|
   | wire string → typed | token 值 → 查询/比较值 | domain · `DecodeMemoryPageToken` 经 `MemorySortTerm.CursorValue`（校验与转换一次完成，typed 存入 entry） | 每 entry 恰一次 |
   | typed → wire string | 文档值 → token 值 | domain · `MemorySortTerm.CursorEntry`（时间 UTC RFC3339Nano 格式化的唯一所在） | 每 key 恰一次（页满构造 next token） |
   | 文档字段 → typed | Mongo 文档 → 游标/查询值 | runtime/mongo · `memoryDocument.sortValue(field) any`（typed 直返，零格式化） | 每 key 恰一次 |
   | typed → bson 子句 | 查询值 → filter | runtime/mongo · `memorySeekClauses` 读 `entry.Typed()` 原样入 bson | 每子句每项引用，零转换 |

   **结论**：游标值的全生命周期责任 = **domain 的 term × entry 对**——term 携带 kind 知识与双向对称转换方法（`CursorValue`/`CursorEntry`），entry 携带 wire/typed 双形态；`string ↔ typed` 往返成套收敛在 domain 单处，仓储只在「文档 typed ↔ entry typed」之间搬运。nil 语义：`*MemoryPageCursor` 为 nil = 首页（handler 空 token 传 nil、仓储 `cursor != nil` 才 seek）；`DecodeMemoryPageToken` 成功恒非 nil（空 token 即错误）。

## §2 快照装载与渲染（JS 客户端）

1. **`MemoryStore.listMemories` 签名**：增可选参数 `options?: { orderBy?: string; pageSize?: number }`：
   - `pageSize` 给定 → **单页即止**：发起一次请求（wire `{parent, pageSize, orderBy}`，undefined 字段不发送），返回首页条目，不续翻——AIP-158 page_size 为该页上限，快照装载只需首页。
   - 未给定 → 现行为：逐页累积至 `next_page_token` 为空（wire `{parent, pageToken}`；写路径全量语义，`operations.ts` 的 2 参调用零改动）。
2. **快照装载**（`service.ts` 的 `load`）：`client.listMemories(template, session, {orderBy: SNAPSHOT_ORDER_BY, pageSize: SNAPSHOT_ENTRY_LIMIT})`——`SNAPSHOT_ORDER_BY = "update_time desc"`、`SNAPSHOT_ENTRY_LIMIT = 10`（注入策略常量由 `snapshot.ts` 所有并导出）。首页即"最近更新的 ≤10 条、服务端有序"（`update_time desc` 在通用机制下归一化为 `[update_time desc, memory_id asc]`，排序形态与契约 §1 一致）；失败 throw（fail-loud 物化回滚，064 §2 既有语义）。
3. **快照渲染**（`renderMemorySnapshot(entries)`）：**纯透传**——按入参顺序逐条一行渲染 `entry.content`；`长期记忆：` 头；空集空串（section 不渲染，既有语义）；`memory_id` 永不渲染（既有义务）。排序与截断由 §1 服务端排序 + 装载查询形态共同承担；客户端不做任何排序/截断/归一化，`MemoryEntry` 仅 `{memory_id, content}`（排序知识单点收敛于服务端）。
4. **冻结时机不变**：`load` 预取时渲染一次并冻结（实例生命周期固定，运行中写入待下次物化生效——064 §2 既有语义）；`applyCall` 写路径与 `old_text` 定位面向全量存储，不传 `orderBy`、全页累积——现行为零变化。

## §3 边界

- 条目数 > 10：装载查询 `page_size=10` 首页即最近 10 条（服务端 page_size 上限语义保证返回不超过请求条数）；被截断条目仍可经 memory 工具操作（存储全量）。
- 全部条目 `update_time` 并列（如一次批量导入）：`memory_id` 升序的前 10 条（§1 收尾规则）。
- 缺省模式消费面：`operations.ts` 写路径、既有 AIP-158 分页断言（`projects/game/testplan/memory_test.go`）继续走 `memory_id` 升序——排序结果与翻页语义零变化；`next_page_token` 形态为通用编码（对客户端不透明、只透传，AIP-158 Opacity），无跨服务版本的持久 token 消费面（写路径翻页循环在单次调用内完成、测试每次从首页开始）。
- `memory_id` 位于键列表非末位（如 `memory_id, update_time desc`）：合法——元组已含唯一键即全序，不追加 tie-breaker（第五轮裁定；消费面当前无此形态，机制为其留正路径）。
- 前端零改动：web 前端无 ListMemories 消费面；proto 字段面向后兼容。

## §4 验收面（对应 spec FR-007、SC-003）

Go 单测（memory 服务）：

- 排序解析（`domain/sort_test.go`，表驱动）：`""` 与纯空白（`"  "`/`"\t"`）→ `[memory_id asc]`（早返回路径）；`"update_time desc"` → `[update_time desc, memory_id asc]`（追加）；空白归一（多空格/制表符/项间空白）；`"update_time desc, memory_id"` → 不追加（幂等）；`"memory_id desc"` → `[memory_id desc]`；**中间位 `memory_id` 合法**——`"memory_id, update_time desc"` → `[memory_id asc, update_time desc]`、`"memory_id desc, update_time"` → `[memory_id desc, update_time asc]`（均原样、不追加、不拒绝——第五轮裁定）；`"update_time"` → `[update_time asc, memory_id asc]`；拒绝：未知字段（`foo`/`content`/`content.foo`）、`"update_time asc"`（非法后缀）、`"update_time desc desc"`、重复字段（`"update_time, update_time"`、`"memory_id, memory_id"`——map 重复检查）；错误信息列受支持字段与语法。
- 游标 codec（`domain/pagination_test.go`）：多键 round-trip（含 RFC3339Nano 纳秒精度、字符串与时间混合键）；**decode 产物 `entry.Typed()` 为 typed 值**——时间 entry 返回解析后的 `time.Time`、字符串 entry 原样（转换单次发生的行为面）；JSON 形态 pin（顶层对象 `{"entries":[{field, value}, ...]}`、时间 UTC RFC3339Nano、`typed` 字段不出现在序列化输出）；坏 token 表驱动（空/坏 base64/坏 JSON/**顶层非对象（如顶层数组）**/`entries` 缺失/`entries` 非数组/`entries` 空/元素 null/缺 field/缺 value/**键不匹配**——长度不符、字段名不符（含未知 field）、跨序重放（desc 序 token 配缺省序、双键 token 配单键序）/时间不可解析）；等价拼写互通（`"update_time desc"` 与 `"update_time desc, memory_id"` 的 token 互相可解）；指针语义（`Decode` 成功返回非 nil `*MemoryPageCursor`；`Encode(nil)` 与空 Entries cursor → 空 token）。
- 仓储（`runtime/mongo/repository_test.go`）：**nil cursor = 首页**（fake `Find` 记录 filter 断言无 `$or`）；缺省 `[memory_id asc]` 回归（序 + limit+1 翻页全量一次——seek 经一个子句的 `$or`）；`update_time desc` 降序、并列 `memory_id` 升序、tie 组跨页边界全序保持；`memory_id desc` 序与翻页（方向翻转比较）；显式两键 `"update_time desc, memory_id"` 与 `"update_time desc"` 结果一致；仓储收到已验证输入后**无校验路径**（fake 记录 sort 与 cursor 即 handler 透传产物）；**单次转换行为面**——fake `Find` 断言 filter 子句中的时间值为 `time.Time` 实例（typed 直达 bson，仓储无 string→time 转换路径；`memorySeekClauses` 无 error 返回是该性质的结构证据）；**防御性转换错误用例不存在**（仓储无转换即无该路径——坏时间值由 domain decode 拒绝，已在 codec 用例覆盖）；**阶梯通用性**——`memorySeekClauses` 直测按任意键数组织：1/2 键用例对照全路径行为，3 键用例以手构 `sort`（`MemorySortTerm` 导出字段 `{Field, MongoField, Descending}`——仓储不校验来源，手构即合法已验证输入）+ entries 经 `term.CursorEntry(...)` 构造（手构 term 零 kind = string 语义；wire-only 字面量手构 entry 的 `Typed()` 为 nil——违约输入，不经此构造），断言产出恰 3 个子句、前缀相等与方向比较逐位正确——pin 增量构造无长度分支（回归为 `switch len(sort)` 即失败）；fake `Find` 求值任意子句数 `$or`（前缀相等 + 方向感知比较）与任意键序排序。
- handler（`handler_test.go`）：合法/非法 `order_by` 表驱动（合法 → 仓储收到归一化最终排序键与已解码 cursor——中间位形态（`memory_id, update_time desc`）收到原样键无追加；**空 token → 仓储收到 nil cursor**；非法 → InvalidArgument）；坏 token / 跨序 token → InvalidArgument（domain 解码拒绝，错误经 `ErrInvalidPageToken` 映射）；单测对 `MemorySortTerm` 的断言仅比较导出字段（`Field`/`MongoField`/`Descending`——kind 为包内私有派生知识），对 cursor 的断言仅比较 `Entries` 的 `{Field, Value}`（`typed` 为私有）。

JS 单测（memory-service，零改动回归）：

- client：`pageSize` 给定单页即止（不续翻、请求 wire 携带 `pageSize`/`orderBy`）；未给定时全页累积回归；条目仅 `{memory_id, content}`。
- service：`load` 发起 `{orderBy, pageSize: 10}` 单页调用且快照按返回序渲染；写路径 2 参调用回归；load 冻结、写后快照不变（064 既有用例）回归。

testplan（`projects/game/testplan/memory_test.go`，经网关查询参数）：

- 有序列表端到端：`?order_by=update_time%20desc` 行为——PATCH 更新后该条目浮至首位、同毫秒并列组内 `memory_id` 升序、`page_size=2` 复合游标续页全量一次且序保持。
- 通用语法正路径：`update_time`（裸字段升序）与 `update_time desc, memory_id`（多字段显式 tie-breaker）合法且序正确——与 `update_time desc` 单页序一致的对照。
- 非法 `order_by` → 400 INVALID_ARGUMENT：`foo`（未知字段）、`update_time asc`（非法后缀）、`content`（未开放字段）。
- 既有缺省模式分页断言（`memory_id` 升序、翻页全量）回归。
- 既有 `TestAgentV2TeamGameStatsPromptFaces`（12 条夹具、间隔 3ms 互异）零改动通过：服务端排序结果与注入断言一致。
