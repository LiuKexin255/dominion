# API 规范

* 使用 REST 风格 API，接口要求符合 [apis](https://google.aip.dev/general) 规范。
* `RPC` 接口使用 `grpc` 协议，`HTTP` 使用 `google apis` 的 `grpc protobuf` 注解。 
* `Service` 和 `Method` 需要注释。`Service` 注释需要包括 `Prefix Path`

## 引用

> 外部规范引用，可作为规范参考。入口索引：[Google API Improvement Proposals (AIPs) — General](https://google.aip.dev/general)。以下按官方分类列出具体 AIP，可直接跳转。

### Meta

* [AIP-1 AIP Purpose and Guidelines](https://google.aip.dev/1)
* [AIP-2 AIP Numbering](https://google.aip.dev/2)
* [AIP-3 AIP Versioning](https://google.aip.dev/3)
* [AIP-8 AIP Style and Guidance](https://google.aip.dev/8)
* [AIP-9 Glossary](https://google.aip.dev/9)
* [AIP-200 Precedent](https://google.aip.dev/200)

### Process

* [AIP-100 API Design Review FAQ](https://google.aip.dev/100)
* [AIP-205 Beta-blocking changes](https://google.aip.dev/205)

### API Concepts

* [AIP-111 Planes](https://google.aip.dev/111)

### Resource Design

* [AIP-121 Resource-oriented design](https://google.aip.dev/121)
* [AIP-122 Resource names](https://google.aip.dev/122)
* [AIP-123 Resource types](https://google.aip.dev/123)
* [AIP-124 Resource association](https://google.aip.dev/124)
* [AIP-126 Enumerations](https://google.aip.dev/126)
* [AIP-128 Declarative-friendly interfaces](https://google.aip.dev/128)
* [AIP-129 Server-Modified Values and Defaults](https://google.aip.dev/129)
* [AIP-156 Singleton resources](https://google.aip.dev/156)
* [AIP-236 Policy preview](https://google.aip.dev/236)

### Operations

* [AIP-130 Methods](https://google.aip.dev/130)
* [AIP-131 Standard methods: Get](https://google.aip.dev/131)
* [AIP-132 Standard methods: List](https://google.aip.dev/132)
* [AIP-133 Standard methods: Create](https://google.aip.dev/133)
* [AIP-134 Standard methods: Update](https://google.aip.dev/134)
* [AIP-135 Standard methods: Delete](https://google.aip.dev/135)
* [AIP-136 Custom methods](https://google.aip.dev/136)
* [AIP-151 Long-running operations](https://google.aip.dev/151)
* [AIP-231 Batch methods: Get](https://google.aip.dev/231)
* [AIP-233 Batch methods: Create](https://google.aip.dev/233)
* [AIP-234 Batch methods: Update](https://google.aip.dev/234)
* [AIP-235 Batch methods: Delete](https://google.aip.dev/235)

### Fields

* [AIP-140 Field names](https://google.aip.dev/140)
* [AIP-141 Quantities](https://google.aip.dev/141)
* [AIP-142 Time and duration](https://google.aip.dev/142)
* [AIP-143 Standardized codes](https://google.aip.dev/143)
* [AIP-144 Repeated fields](https://google.aip.dev/144)
* [AIP-145 Ranges](https://google.aip.dev/145)
* [AIP-146 Generic fields](https://google.aip.dev/146)
* [AIP-147 Sensitive fields](https://google.aip.dev/147)
* [AIP-148 Standard fields](https://google.aip.dev/148)
* [AIP-149 Unset field values](https://google.aip.dev/149)
* [AIP-202 Fields](https://google.aip.dev/202)
* [AIP-203 Field behavior documentation](https://google.aip.dev/203)
* [AIP-216 States](https://google.aip.dev/216)

### Design Patterns

* [AIP-152 Jobs](https://google.aip.dev/152)
* [AIP-153 Import and export](https://google.aip.dev/153)
* [AIP-154 Resource freshness validation](https://google.aip.dev/154)
* [AIP-155 Request identification](https://google.aip.dev/155)
* [AIP-157 Partial responses](https://google.aip.dev/157)
* [AIP-158 Pagination](https://google.aip.dev/158)
* [AIP-159 Reading across collections](https://google.aip.dev/159)
* [AIP-160 Filtering](https://google.aip.dev/160)
* [AIP-161 Field masks](https://google.aip.dev/161)
* [AIP-162 Resource Revisions](https://google.aip.dev/162) (Draft)
* [AIP-163 Change validation](https://google.aip.dev/163)
* [AIP-164 Soft delete](https://google.aip.dev/164)
* [AIP-165 Criteria-based delete](https://google.aip.dev/165)
* [AIP-210 Unicode](https://google.aip.dev/210)
* [AIP-211 Authorization checks](https://google.aip.dev/211)
* [AIP-214 Resource expiration](https://google.aip.dev/214)
* [AIP-217 Unreachable resources](https://google.aip.dev/217)

### Compatibility and Versioning

* [AIP-180 Backwards compatibility](https://google.aip.dev/180)
* [AIP-181 Stability levels](https://google.aip.dev/181)
* [AIP-182 External software dependencies](https://google.aip.dev/182) (Reviewing)
* [AIP-185 API Versioning](https://google.aip.dev/185)

### Polish

* [AIP-190 Naming conventions](https://google.aip.dev/190)
* [AIP-191 File and directory structure](https://google.aip.dev/191)
* [AIP-192 Documentation](https://google.aip.dev/192)
* [AIP-193 Errors](https://google.aip.dev/193)
* [AIP-194 Automatic retry configuration](https://google.aip.dev/194)

### Protocol buffers

* [AIP-127 HTTP and gRPC Transcoding](https://google.aip.dev/127)
* [AIP-213 Common components](https://google.aip.dev/213)
* [AIP-215 API-specific protos](https://google.aip.dev/215)