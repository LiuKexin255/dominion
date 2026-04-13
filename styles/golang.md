# golang 代码风格

* 在满足规范要求的前提下，保持仓库代码**风格统一**。

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

* 对于**无初始化**的指针对象创建，使用关键字 `new`。

```golang
// good case
a := new(A)

// bad case
a := &A{}
```

* 对于带有初始值的初始化，使用字面量创建

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

* 当需要将值类型赋给指针类型时，使用 `&` 或 `toPtr` 操作。

```golang
// good case
var a A 
var need *A

need = &a
// or 定义一个转换指针方法 toPtr 
need = toPtr(a)
```

* 文件名、目录、环境变量等字面量，应当定义常量避免魔术字。
* 枚举值应当定义自己的类型。
* 仅在**有需要**时，才对结构体、数组和字典进行深拷贝。不要对无修改影响的对象进行深拷贝。

```golang
// bad case 
func foo() []string {
	var a []string 
	a = otherFunc() 
	// deep copy is unnecessary
	return deepcopy(a)
}

```

## 注释

* `package` 可导出对象（变量、方法、结构体）需要按 `golang` 注释方式说明功能、入参出参含义以及注意事项。
* `package` 级别内部变量和 `type` 定义类型需增加注释。
* 复杂代码逻辑、关键步骤或易错地方，应增加注释以说明原因和注意点。

## 函数

* 对入参、结构体 Revicer 校验仅限于函数自身功能内需要，不对参数做过度或不是本参数（包）负责的校验。特别是传入的参数已经在本仓库的其他包内进行校验，不要做重复校验。

## 单元测试 

重要原则：

* 测试代码主体应当完整，包含如何从输入得到输出结果的所有信息。不要将重要信息隐藏在 `helper` 方法中。复杂的测试数据可以保存到 `testdata` 下。

	> 有一些情况适合使用 `helper` 构造测试数据，例如测试不关心的特定参数或用例，可以用 `helper` 方式并复用。

* 测试状态（结果）而不是交互；测试行为而不是方法。
* 一个测试函数的测试用例应该共享同一个测试逻辑，避免使用过多的流程控制（如 `if/switch`）。如果某个函数需要多种测试逻辑，则拆分成多个测试函数。
* **禁止**在测试用例中塞入断言逻辑。使用 `helper` 进行验证时，应对单一概念或对象进行断言，而不是一组固定的检查。

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