# 单元测试风格

## 使用表驱动风格

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

## 不能访问外部依赖

单测代码不能通过网络访问非本机的依赖，例如数据库、http 网站或者部署在其他机器上的服务。