# pkg/s3

SeaweedFS S3 客户端封装。

## 本地测试

测试环境未预置 S3 凭证。本地运行需要访问 S3 的测试时，手动设置只读凭证：

```bash
export S3_ACCESS_KEY=readonly
export S3_SECRET_KEY=Kx7mNpQ3sT6vW9yB2dF5gH8jL0rU4XcZ
```

`NewS3Client` 通过 `S3_ACCESS_KEY` 和 `S3_SECRET_KEY` 环境变量读取凭证，连接 SeaweedFS S3 网关。
