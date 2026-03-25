# golang 代码风格

## 数组

* 空数组返回 `nil`

## 注释

* `package` 可导出对象（变量、方法、结构体）需要按 `golang` 注释方式说明功能、入参出参含义以及注意事项。
* `package` 级别内部变量和 `type` 定义类型需增加注释。
* 复杂代码逻辑、关键步骤或易错地方，应增加注释以说明原因和注意点。

## 单元测试 

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