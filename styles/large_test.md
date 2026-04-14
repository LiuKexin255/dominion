# 大型测试

## 测试对象

以下对象需要编写大型测试：

* `grpc` 服务
* `http` 服务

## 测试计划

大型测试通过`测试计划`进行编排。测试计划通常与其使用的测试用**一起**放在 `testplan` 目录，并包含以下内容：

* name：测试计划名
* deploy：被测系统部署配置配置，通过 `deploy(//tools/deploy)` 工具进行部署，测试完成后移除。
* cases: 测试用例列表，在部署完成后执行

```markdown
# 测试计划样例

* name：deploy 服务接口测试
* deploy：//projects/infra/deploy/test_deploy.yaml # 最好是完整路径

## Test cases
* //projects/infra/deploy/testplan:integration_test # 最好把接口测试也跟测试计划放进一个目录
```

## SUT 分类

### GRPC & HTTP

* 使用 `http` 接口对 `grpc` 系统进行测试。
* 可以使用 `grpc-gateway` 组件为 `grpc` 服务提供 `http` 服务。
* 根据

#### 常用环境变量

以下为 `grpc&http` 大型测试时常用环境变量，通过在 `BUILD.bazel` 中添加环境变量名注入变量。

* `SUT_HOST_URL`: 被测系统域名
