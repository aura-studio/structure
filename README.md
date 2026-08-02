# structure

在 **JSON / YAML / TOML / Lua / Python / JavaScript** 六种文本格式之间互相转换结构化数据。文本进，文本出，一次调用。

```go
out, err := structure.Convert(input, structure.JSON, structure.YAML)
```

- 保序：映射键顺序在保序格式之间原样传递
- 数字保真：`int64 → uint64 → *big.Int` 三级承载，2^53 以上整数不失精度
- 原生 any 互通：`FromAny` / `ToAny` 与 `map[string]any` / `[]any` 双向转换（丢序，见下）
- 只解析不执行：Lua / Python / JavaScript 走纯字面量子集，AST 白名单解释，永不执行代码
- 并发安全：无包级可变状态

## 安装

```bash
go get github.com/aura-studio/structure/v2
```

要求 Go 1.25 及以上。

## 包结构

根包 `structure` 是门面（facade）。数据模型、格式枚举、六个编解码器各自独立成包：

```
structure          门面：Parse / Encode / Convert / ParseFormat / FromAny / ToAny，按格式分派
├── node           数据模型：Node、OrderedMap、承载阶梯、原生 any 转换、深度与合法性校验、哨兵错误
├── format         格式枚举与 ParseError（不 import node，两个独立的根）
├── codec
│   ├── json  yaml  toml  lua  python  js          每个包一对 Parse / Encode
│   └── internal/emit                              缩进等编码器共用的小工具
├── internal/nodetest                              各包共用的夹具与深嵌套生成器
└── tests                                          门面的测试与 fuzz 语料
```

依赖是单向的：`node` 与 `format` 互不相识，`codec/*` 依赖两者，门面依赖全部。唯一一条 codec 到 codec 的边是 `codec/js` → `codec/json`（JS 编码器复用 JSON 渲染器，两边共用同一套转义，避免各自漂移）。

`tests` 是一个独立的测试包，本身不含任何库代码，也没有任何包 import 它。它只能通过公开面触达门面，所以它同时充当门面 API 的可用性检验：任何一处该导出而没导出的东西，在这里会直接编译不过。

门面里的每个导出名都是**类型别名**或一行转发，不是新定义的类型：

```go
type Node = node.Node
type OrderedMap = node.OrderedMap
type ParseError = format.ParseError
```

所以 `structure.OrderedMap` 与 `node.OrderedMap` 是同一个类型，`errors.As` 用两种写法都能匹配，`json.Marshaler` 检测与 type switch 也一致。只用一种格式就直接 import 对应子包，需要按格式分派再用门面。

未来的扩展（比如 merge）落在新的子包里，不动这条依赖链。

## API

文本进出的三个函数，`Convert` 等于 `Parse` 接 `Encode`：

```go
// 直接转换
yamlText, err := structure.Convert(jsonText, structure.JSON, structure.YAML)

// 分两步，中间可以检查或改写 Node 树
node, err := structure.Parse(tomlText, structure.TOML)
if err != nil {
    return err
}
node.(*structure.OrderedMap).Set("extra", int64(1))
out, err := structure.Encode(node, structure.Python)

// 从字符串解析格式名（大小写不敏感，支持 yml / py / python3 / javascript / ecmascript 别名）
f, err := structure.ParseFormat("yml") // => structure.YAML
```

### 原生 Go any 互通

`FromAny` / `ToAny` 在 `map[string]any` / `[]any`（也就是 `json.Unmarshal` 产出的形状）与 `Node` 之间双向转换：

```go
// 编码前转一次
n, err := structure.FromAny(map[string]any{"a": 1, "b": []any{true, "x"}})
out, err := structure.Encode(n, structure.YAML)

// 解析后转回来
n, err = structure.Parse(jsonText, structure.JSON)
v := structure.ToAny(n) // => map[string]any
```

`Parse` / `Encode` 的签名与行为**零改动**：`Encode` 依然拒绝裸 `map[string]any`，`Parse` 也依然只产出 `*OrderedMap`。转换是调用方在边界上显式做的一步，不是隐式强转——所以模型内部一个映射永远只有一种表示。

- **整个 int / uint 族都收**（含无类型常量默认的 `int`），统一落到 `int64 → uint64 → *big.Int` 阶梯；`uint` 族一律经 `uint64`，不会出现 `int64(uint64(1<<63))` 静默变负
- `json.Number` 收（配 `Decoder.UseNumber` 就能把 2^53 以上的整数原样带进来），`float32` 加宽为 `float64`
- 输入里已有的 `*OrderedMap` **保留插入顺序**，只有本来无序的 Go map 才排序
- 其余类型**按名字拒绝**：`map[any]any`、`map[string]string`、`[]string`、`[]byte`、数组、struct、指针、`time.Time`、`time.Duration`、`big.Float`、chan、func、complex，以及任何具名类型。刻意不设 `fmt.Stringer` / `error` 兜底——这些类型全都有 `String()`，兜底会把 `1<<100` 从正确的整数静默变成带引号的字符串，还能通过 `Validate`
- 输入不必是容器：转换器不管根形状，`FromAny(42)` 得到 `int64(42)`，顶层标量的拒绝仍由 `Validate` / `Encode` 负责。UTF-8 校验同理

### Node 模型

`Parse` 返回、`Encode` 接受的中间表示是 `Node`（即 `any`），合法承载类型恰好这几种：

| 类型 | 说明 |
|---|---|
| `*OrderedMap` | 映射，字符串键，保留插入顺序 |
| `[]Node` | 数组 |
| `nil` | null / None / nil |
| `bool` | 布尔 |
| `int64` | 有符号整数（首选承载） |
| `uint64` | 超出 int64 的无符号整数 |
| `*big.Int` | 超出 uint64 的任意精度整数 |
| `float64` | 浮点；NaN/±Inf 只有 YAML 和 TOML 可编码 |
| `string` | 文本；时间/日期一律规范化为字符串 |

其他 Go 类型一律被 `Encode` 拒绝。文档根必须是映射或数组，裸标量顶层返回 `ErrTopLevelScalar`（这是「不包含纯单值不嵌套结构」的落地）。

输入输出都必须是合法 UTF-8：`Parse` 在分派前校验并给出出错位置，`Encode` 校验字符串值与映射键。六种格式的规范都要求 UTF-8 源文本，而按字节工作的 Python / Lua 解析器会把野字节带进字符串，编码器按 rune 遍历时又会静默替换成 U+FFFD——这条往返静默损坏是 `FuzzRoundTrip` 发现的，现已在两端堵住，语料保留为回归种子。

`OrderedMap` 是 map + 双向链表，`Get`/`Set`/`Delete`/`Len` 均 O(1)，`Set` 已存在的键就地更新不改变位置。它提供 `All() iter.Seq2[string, Node]` 供 range-over-func 遍历，`Clone()` 深拷贝，以及保序的 `MarshalJSON()`。

### 错误

```go
var pe *structure.ParseError
if errors.As(err, &pe) {
    fmt.Println(pe.Format, pe.Line, pe.Column, pe.Msg)
}
```

哨兵错误，全部支持 `errors.Is`：

| 哨兵 | 含义 |
|---|---|
| `ErrUnsupportedStructure` | 目标格式无法表示该结构（同时匹配 `errors.ErrUnsupported`） |
| `ErrTooDeep` | 嵌套超过 10000 层 |
| `ErrTopLevelScalar` | 根不是映射或数组 |
| `ErrDuplicateKey` | 重复映射键（JSON / YAML / Python 拒绝） |

## 格式能力矩阵

编码侧的拒绝项：

| 目标格式 | 顶层数组 | nil 值 | NaN/±Inf | 其他限制 |
|---|:---:|:---:|:---:|---|
| JSON | ✅ | ✅ | ❌ | — |
| YAML | ✅ | ✅ | ✅ | — |
| TOML | ❌ 根须是表 | ❌ 无 null | ✅ | 整数上限 int64，uint64/big.Int 越界报错 |
| Lua | ✅ | 数组元素 ❌ / map 键跳过 | ❌ | 哈希段键排序输出 |
| Python | ✅ | ✅ | ❌ | — |
| JS | ✅ | ✅ | ❌ | 输出即 JSON 文本 |

解析侧的子集约定：

- **Lua**：`parsing.ParseExp` 解析表构造器字面量。表要么是纯数组段，要么是纯字符串键哈希段，混合报错；函数、调用、算术表达式一律拒绝；`k = nil` 按 Lua 语义删除该键。
- **Python**：手写递归下降，对齐 `ast.literal_eval`。支持三引号、`r`/`u` 前缀、完整转义表、`0x`/`0o`/`0b`、PEP 515 下划线分隔、元组（规范化为数组）。拒绝 bytes、f-string、set、complex、`\N{name}`、隐式字符串拼接、非字符串 dict 键。
- **JavaScript**：goja 的纯解析器（只 `ParseFile`，绝不 `RunString`），AST 白名单解释。对象/数组/字符串/数字/布尔/null/一元 ±数字之外全部拒绝；数组空位（elision）变 `nil`；`__proto__` 当普通键。
- **YAML**：yaml.v3 的 `yaml.Node` 路线。不支持多文档、锚点、别名、合并键、自定义 tag、`!!binary`。超出 uint64 被 yaml.v3 降级为 `!!float` 的整数会按原文还原成 `*big.Int`。
- **TOML**：BurntSushi 解析，再用 `MetaData.Keys()` 的文档序把无序 map 回放成有序树（表数组的每个元素靠游标定位，嵌套 `[[a.b]]` 在每个新 `a` 元素里重新计数）。内联表没有可恢复的顺序，按键名排序保证确定性。四种时间类型全部规范化为字符串。

## 已知限制

1. YAML 不保留注释、锚点、别名、合并键、多文档、自定义 tag、`!!binary`。`%YAML 1.2` 指令被拒绝：yaml.v3 只实现 1.1，吞掉指令按 1.1 解析会静默改变 `y`/`no`、八进制等标量的含义，报错比静默降级安全。
2. Lua 只支持纯数组表与纯字符串键字典表；按 Lua 5.3 词法解析，不保证 5.4 新语法。
3. Python dict 键冻结为字符串；不支持 bytes / set / complex / Ellipsis / `\N{name}` / 隐式拼接。
4. JS 输出即 JSON 文本（是合法的 JS 子集）；数字样键在真实 JS 引擎里会被重排（ECMA-262 §10.1.11.1 的运行时行为，非本库缺陷，生成文本仍按插入序）。
5. goja 以伪版本固定（上游无 semver tag），其 `ast` 包自述接口可能变更，升级需回归。
6. yaml.v3 仓库已 archived；v3.0.1 代码稳定、零依赖，仍是事实标准。`OrderedMap` 层已隔离该依赖，长期可评估切换。
7. TOML 时间值转换后是字符串（语义降级，往返不等型）。
8. 深度上限 10000 硬编码（对齐标准库 `encoding/json` 与 `encoding/xml` 先例），不可配置。Go 的栈溢出不可 recover，所以这个上限由各解析器显式检查。
9. 保序只对 JSON / YAML / Python / JS 有语义保证。TOML 表内顺序、Lua 哈希顺序在各自规范里都无意义，本库输出确定但不承诺与输入一致。
10. 输入输出一律要求合法 UTF-8，不支持其他编码，也不为野字节提供逃逸表示。需要处理二进制请先自行转成 base64 等文本形式。
11. 不含 XML。XML 的数据模型（属性、混合内容、单根、值无类型）与这里的 Node 模型不同构，硬塞进来只能靠 `@attr`/`#text` 这类约定，往返语义不干净，故整包移除。
12. `FromAny` / `ToAny` 一律丢映射顺序，因为 Go 的 map 装不住顺序。`ToAny` 方向无从补救（要顺序就留着 `Node`）；`FromAny` 方向用 `sort.Strings` 换来确定性——六个编码器里五个严格按插入序输出，裸 `range` 一个 Go map 会让同一个程序两次运行的输出不一样。
13. `FromAny` 的输入必须是树。自引用环会撞上 `MaxDepth` 变成可恢复的 `ErrTooDeep`，但**共享的无环子树（DAG）不去重**：每个引用都会被展开，60 层同一个子树出现两次就是 2^60 条路径。有共享结构请自己先去重。

## 测试

```bash
go test ./...              # 全量
go test -race ./...        # 竞态
bash scripts/coverage.sh   # 覆盖率门禁（默认 95%）
go test -bench=. ./...     # 基准
```

测试按符号归属分布：某个断言测什么符号，就放在那个符号所在的包里。`node` 的承载与校验测试在 `node/`，`format` 的枚举与位置计算在 `format/`，每种格式的解析/编码分支在 `codec/<格式>/`，其中依赖未导出符号的少数用例（如 TOML 的游标编码、YAML 的 `plainSafe`、各编码器 `validate` 之后不可达的防御分支）用同包测试直接驱动。夹具集中在 `internal/nodetest`，各包共用。

门面自己的契约——跨格式一致性、格式归属、`Parse`/`Encode` 两端的整篇拒绝——放在 `tests/`。根包目录下没有测试文件，所以 `structure.go` 的覆盖率靠 `tests/` 经 `-coverpkg=./...` 回填；`go test ./...` 里根包显示 `[no test files]` 是预期的。这样做的代价是门面的未导出符号不能再被测试直接调用，收益是这些断言只能走公开面，与真实调用者同路。

测试面：

- **36 格转换矩阵**：六格式两两互转全组合，共享嵌套/顶层数组/空容器夹具，TOML 目标附带顶层数组的错误期望。
- **Round-trip**：每格式 `Parse(Encode(n))` 深等值。比较器 `nodeEqual` 对保序格式逐位置比键，对无序格式只比键值；浮点按位比较且 NaN==NaN；整数跨 `int64`/`uint64`/`*big.Int` 承载做数值比较。
- **等价链**：`JSON→YAML→TOML→JSON ≡ JSON→TOML→JSON`；另有 `JSON→Lua→JSON` 一条，用无序比较验证丢序格式仍然保值。
- **错误面**：每格式的非法语法、顶层标量、超深（10001 层 → `ErrTooDeep`，1000 层通过）、重复键，以及各格式特有的拒绝项。
- **原生 any 转换**：逐类型钉承载（不只比数值——`uint` 落 `int64` 或 `int` 落 `float64` 都会数值相等而破坏阶梯）、拒绝清单逐类型、深度边界 10000/10001、自引用环与相互引用、不改动调用方输入、排序在 50 次重复下稳定（Go 随机化 map 遍历，未排序的实现几次内就会红）。门面侧另测转换结果能喂给全部六个编码器，以及 `ToAny` 输出能过 `json.Marshal`。
- **Fuzz**：`FuzzParse`（断言永不 panic、根必为容器、返回的 Node 通过 `validate`）与 `FuzzRoundTrip`（Encode→Parse→深等值）；`tests/testdata/fuzz/FuzzParse/` 每格式 ≥3 条真实种子（语料按包目录定位，所以它跟着测试一起放在 `tests/` 下）。种子的第一个字节是格式选择器，按 `Format` 序数索引，所以序数只增不改（移除 XML 时做过一次紧凑重排，语料在同一个提交里同步改了选择字节；`format` 包有测试把序数钉死）。
- **Benchmark**：各格式 Parse/Encode 与代表性 Convert 路径，`b.Loop()` + `ReportAllocs` + `SetBytes`。

覆盖率门禁默认 95%，可用环境变量覆盖：

```bash
COVERAGE_THRESHOLD=97 bash scripts/coverage.sh
```

## 依赖

| 依赖 | 用途 |
|---|---|
| `gopkg.in/yaml.v3` | YAML 的 `yaml.Node` 保序解析与编码 |
| `github.com/BurntSushi/toml` | TOML 解析（`MetaData.Keys()` 提供文档序） |
| `github.com/arnodel/golua` | Lua 表达式解析器（纯解析，无 VM） |
| `github.com/dop251/goja` | JavaScript 解析器（只用 `parser`/`ast`，不启动运行时） |

JSON 走标准库，TOML、Lua、Python、JS 的编码器全部手写。
