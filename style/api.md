# API 规范

* 使用 REST 风格 API，接口要求符合 [apis](https://google.aip.dev/general) 规范。
* `RPC` 接口使用 `grpc` 协议，`HTTP` 使用 `google apis` 的 `grpc protobuf` 注解。 
* `Service` 和 `Method` 需要注释。`Service` 注释需要包括 `Prefix Path`