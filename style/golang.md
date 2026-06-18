# golang 代码风格

* 在满足规范要求的前提下，保持仓库代码**风格统一**。

## 引用

* 包引用使用三级分组，从上到下分别是`标准库`、`当前仓库包`、`第三方库`
* 依赖重命名按照分组顺序，由低到高进行重命名。例如当第三方库与当前仓库包重名，对第三方库进行重命名。
* 非必要不进行重命名，**仅在出现重复包名时才重命名**，特别是不应进行同名的重命名动作。

```golang
// bad case，alias the same name
import pkga "xxx.com/group/repo/pkga"
```

## 数组

* 函数返回空数组（长度为 0 数组）时返回 `nil`
* 数组与 `map` 初始化除非必要，否则不加长度参数

```golang
// good case
var a []string
m := make(map[string]string)
m := map[string]string{}

// bad case
a := make([]string , 0 , len(...))
m := make(map[string]string, 0, len(...))

```

## 变量

* 文件名、目录、环境变量等字面量，应当定义常量避免魔术字。
* 枚举值应当定义自己的类型。

## 指针

### 基本原则：对象语义与元组语义

结构体是否使用指针类型，首先取决于语义：

* **对象语义**：结构体代表一个具有身份的"实例"（如 `Customer`、`Order`、`Message`），某个具体对象不同于另一个。优先使用**指针**。
* **元组语义**：结构体仅是一组值的聚合，值本身即含义（如 `Point{X, Y}`、`Range{Start, End}`），不存在"实例"概念。优先使用**值类型**。

在此基础上，根据结构体所处的上下文（容器元素、函数签名、结构体字段）适用以下具体规则。

### 容器元素（slice / map）

强制使用指针类型 `[]*T`、`map[K]*T`。

* slice 和 map 的元素在 `append`、`range`、按 key 读取时都会发生值拷贝。
* 将 `nil` 放入 slice 或 map 属于反模式——迭代容器本身应当等价于"元素全部有效"，因此使用指针不会引入额外的 nil 检查负担。
* 基本类型（`int`、`string`、`bool` 等）的容器不受此限。

```golang
// good
type Foo struct {
	Items []*Bar
	Index map[string]*Bar
}
messages := []*Message{}

// bad
type Foo struct {
	Items []Bar
	Index map[string]Bar
}
```

### 函数参数与返回值

优先使用指针类型。

* 值传递会触发完整结构体拷贝；当结构体包含指针字段时，值传递仅产生浅拷贝，无法保证调用方隔离。
* 若调用方需要快照或隔离，应由**函数内部显式深拷贝**，而非依赖返回值的值语义。
* 元组语义的结构体（如坐标 `Point`）使用值类型——指针不符合语义。

```golang
// good
func parseMessage(data []byte, path string) (*Message, error) {}

// good — 元组语义，值类型符合语义
func center() Point {}

// bad
func parseMessage(data []byte, path string) (Message, error) {}
```

### 结构体属性字段

按语义和空值频率区分：

**优先值类型**：配置对象、DTO 等**空值/零值是常态**且**不涉及频繁拷贝**的字段。使用指针会导致大量 nil 检查，而数据本身很少被复制到其他对象。

```golang
// good — config 字段，空值是常态，不频繁拷贝
type ServerConfig struct {
	Logging  LoggingConfig
	Tracing  TracingConfig
}

// bad — config 字段用指针，到处需要 nil 检查
type ServerConfig struct {
	Logging  *LoggingConfig
	Tracing  *TracingConfig
}
```

**优先指针类型**：领域模型等**需要共享同一实例**、**会被频繁传递**、或**零值无业务意义**的字段。

```golang
// good
type Order struct {
	Customer *Customer
	Items    []*Item
}

// bad — Customer 是领域对象，按值拷贝会导致别名问题
type Order struct {
	Customer Customer
}
```

### 深拷贝

仅在**有需要**时，才对结构体、数组和字典进行深拷贝。不要对无修改影响的对象进行深拷贝。

```golang
// bad case
func foo() []string {
	var a []string
	a = otherFunc()
	// deep copy is unnecessary
	return deepcopy(a)
}
```

### 指针对象的构造

对于**无初始化**的指针对象创建，使用关键字 `new`。

```golang
// good case
a := new(A)

// bad case
a := &A{}
```

对于带有初始值的初始化，使用字面量创建。

```golang
// good case
a := A{
	Foo: "value",
	Bar: 123,
}

aPtr := &A{
	Foo: "value",
	Bar: 123,
}

// not recommended
aPtr := new(A)
aPtr.Foo = "value"
aPtr.Bar = 123
```

当需要将值类型赋给指针类型时，使用 `&` 或 `toPtr` 操作。

```golang
// good case
var a A
var need *A

need = &a
// or 定义一个转换指针方法 toPtr
need = toPtr(a)
```

## 注释

* `package` 可导出对象（变量、方法、结构体）需要按 `golang` 注释方式说明功能、入参出参含义以及注意事项。
* `package` 级别内部变量和 `type` 定义类型需增加注释。
* 复杂代码逻辑、关键步骤或易错地方，应增加注释以说明原因和注意点。

## 函数

* 对入参、结构体 Revicer 校验仅限于函数自身功能内需要，不对参数做过度或不是本参数（包）负责的校验。特别是传入的参数已经在本仓库的其他包内进行校验，不要做重复校验。

## 结构体

* `HTTP` 请求如果是同仓库内的 `grpc` 接口（通常都是 `grpc proto` 定义，使用 `grpc-gateway` 转换），其入参出参使用 `proto` 对象，并使用 `protojson` 进行序列化和反序列化（反序列化设置 `DiscardUnknown = true`）。 

## 可观测

* 服务代码需要使用 `otel` 上报 `tracing`、`log` 以及 `metrics` 信息。
* `Info` 日志目的在于观察系统正常运行，以及出错后进行快速定位。
* `Error` 日志用于错误日志输出。
* 日志打印不要散落在代码各处，也不要重复打印以免干扰。最好都集中在`顶层`或`逻辑入口`处，做到*兜底*和*便于管理*。

## 目录结构

### grpc-go

* 服务根目录：proto，service 定义
* 常用分层 app、handler、service、domain、runtime、pkg
* app：服务与依赖的启动与组装。
* hanlder：负责实现服务 proto interface，与 `grpc-go` 框架和实现方式有关的逻辑集中在这一层。
* service：无状态、过程式方法，以及操作领域模型的纯逻辑代码。
* domain：领域模型
* runtime：各类依赖实现，例如其他服务、数据库等
* pkg：公共库，通常满足两个特征：1. 非定制，代码仅作为某种算法/方法的实现。2. 不同层会多次使用。例如某些常量、ID 生成等。

## 单元测试 

重要原则：

* 测试代码主体应当完整，包含如何从输入得到输出结果的所有信息。不要将重要信息隐藏在 `helper` 方法中。复杂的测试数据可以保存到 `testdata` 下。

	> 有一些情况适合使用 `helper` 构造测试数据，例如测试不关心的特定参数或用例，可以用 `helper` 方式并复用。

* 测试状态（结果）而不是交互；测试行为而不是方法。
* 一个测试函数的测试用例应该共享同一个测试逻辑，避免使用过多的流程控制（如 `if/switch`）。如果某个函数需要多种测试逻辑，则拆分成多个测试函数。
* **禁止**在测试用例中塞入断言逻辑。使用 `helper` 进行验证时，应对单一概念或对象进行断言，而不是一组固定的检查。
* * 单测 `target` 名最好使用 `gazelle` 生成的默认名称（`{package_name}_test`），以防止重复生成 `go_unittest`。

```golang
// 例如测试 add 方法
func add (a int , b int) int {
	return a + b
}

// good test  

good_case := &case {
	param: &param{
		a : 2,
		b : 3,
	},
	want: 5 // 可以通过测试代码看出，5 是如何得到的。
}

got := add(good_case.param.a, good_case.param.b)
if got != good_case.want {
	// test error
}

// bad test 
bad_case := &case {
	param: build_hepler(),
	want: 5 // 无法看出为什么结果是 5。
}
```

### 命名风格

* 导出函数使用 `TestFuncName` 作为单测函数名，非导出函数使用 `Test_funcName` 作为单测函数名。 


### 使用表驱动风格

单测试用例使用 `Plain Mode`，多测试用例使用表驱动风格。测试函数需要包括 `given/when/then`（也可以称 `arrange/act/assert`）:

* `given`: `tests` 提供测试用例与所需信息。
* `when`: 执行 `parseOptions` 解析参数。
* `then`: 解析**是/否**成功。

```golang
func TestParseOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "env only", args: []string{"--env=dev"}},
		{name: "deploy with env", args: []string{"--deploy=deploy.yaml", "--env=dev"}},
		{name: "delete only", args: []string{"--del=dev"}},
		{name: "missing args", args: nil, wantErr: true},
		{name: "deploy without env", args: []string{"--deploy=deploy.yaml"}, wantErr: true},
		{name: "delete with env", args: []string{"--del=dev", "--env=dev"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOptions(tt.args)
			if tt.wantErr && err == nil {
				t.Fatalf("parseOptions(%v) expected error", tt.args)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("parseOptions(%v) unexpected error: %v", tt.args, err)
			}
		})
	}
}
```

### 不能访问外部依赖

单测代码不能通过网络访问非本机的依赖，例如数据库、http 网站或者部署在其他机器上的服务。

## 引用

> 外部的规范引用，可作为规范参考

* [Google Go Style](https://google.github.io/styleguide/go/)