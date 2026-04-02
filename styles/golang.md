# golang 代码风格

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

## 注释

* `package` 可导出对象（变量、方法、结构体）需要按 `golang` 注释方式说明功能、入参出参含义以及注意事项。
* `package` 级别内部变量和 `type` 定义类型需增加注释。
* 复杂代码逻辑、关键步骤或易错地方，应增加注释以说明原因和注意点。

## 函数

* 对入参、结构体 Revicer 校验仅限于函数自身功能内需要，不对参数做过度或不是本参数（包）负责的校验。特别是传入的参数已经在本仓库的其他包内进行校验，不要做重复校验。

## 单元测试 

### 命名风格

* 导出函数使用 `TestFuncName` 作为单测函数名，非导出函数使用 `Test_funcName` 作为单测函数名。 

### 测试逻辑

* 一个测试函数的测试用例应该共享同一个测试逻辑。如果某个函数需要多种测试逻辑，则拆分成多个测试函数。
* **禁止**在测试用例中塞入断言逻辑。

### 使用表驱动风格

单元测试风格如下：

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