# common/gopkg/s3

SeaweedFS S3 客户端封装。

## 使用

```go
import "dominion/common/gopkg/s3"

// 从环境变量读取凭证（默认行为）
client, err := s3.NewS3Client()

// 通过 option 显式指定 region、accessKey、secretKey
client, err := s3.NewS3Client(
    s3.WithRegion("us-east-1"),
    s3.WithAccessKey("readonly"),
    s3.WithSecretKey("Kx7mNpQ3sT6vW9yB2dF5gH8jL0rU4XcZ"),
)
```

`NewS3Client` 支持通过 `ClientOption` 函数式选项配置：
- `WithRegion(string)` — 设置 S3 region，未设置时默认为 `us-east-1`
- `WithAccessKey(string)` — 设置 access key，未设置时从环境变量 `S3_ACCESS_KEY` 读取
- `WithSecretKey(string)` — 设置 secret key，未设置时从环境变量 `S3_SECRET_KEY` 读取
