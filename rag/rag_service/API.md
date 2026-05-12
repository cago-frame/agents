# RAG Service API 文档

所有业务接口以 `/api/v1` 为前缀。服务启动后可通过 `http://localhost:8000/docs` 访问交互式 Swagger 文档。

## 生成向量嵌入

```
POST /api/v1/embeddings
```

将文本列表转换为向量嵌入。

**请求体：**

```json
{
  "texts": ["文本1", "文本2"]
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `texts` | string[] | 是 | 待嵌入的文本列表（至少 1 条） |

**响应：**

```json
{
  "model": "text-embedding-3-small",
  "dimension": 1536,
  "embeddings": [
    {"index": 0, "vector": [0.01, ...], "token_count": 5},
    {"index": 1, "vector": [0.02, ...], "token_count": 4}
  ],
  "total_tokens": 9
}
```

---

## 上传文档

```
POST /api/v1/documents
```

以 `multipart/form-data` 形式上传文件，自动完成 解析 → 分块 → 嵌入 → 存储 全流程。

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `file` | File | 是 | 上传的文件 |
| `index_name` | string | 是 | 目标索引名称 |
| `metadata` | string (JSON) | 否 | 附加元数据 |

**响应：**

```json
{
  "document_id": "uuid-string",
  "chunk_count": 42,
  "index_name": "rag_my_index"
}
```

---

## 删除文档

```
DELETE /api/v1/documents
```

删除指定文档及其所有分块。

**请求体：**

```json
{
  "document_id": "uuid-string",
  "index_name": "rag_my_index"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `document_id` | string | 是 | 文档 ID |
| `index_name` | string | 是 | 文档所在索引 |

**响应：**

```json
{
  "document_id": "uuid-string",
  "deleted_chunks": 42,
  "index_name": "rag_my_index"
}
```

---

## 语义搜索

```
POST /api/v1/search
```

对指定索引进行向量相似度搜索。

**请求体：**

```json
{
  "query": "查询文本",
  "index_name": "rag_my_index",
  "top_k": 10,
  "min_score": 0.5,
  "filters": {"filename": "report.pdf"}
}
```

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `query` | string | 是 | - | 搜索查询文本 |
| `index_name` | string | 是 | - | 目标索引名称 |
| `top_k` | int | 否 | `10` | 返回结果数量（1-100） |
| `min_score` | float | 否 | `0.0` | 最低相似度阈值（0-1） |
| `filters` | object | 否 | `null` | 元数据过滤条件 |

**响应：**

```json
{
  "query": "查询文本",
  "index_name": "rag_my_index",
  "hits": [
    {
      "document_id": "uuid-string",
      "score": 0.92,
      "content": "匹配的文本片段...",
      "metadata": {"filename": "report.pdf", "chunk_index": 3}
    }
  ],
  "total": 5
}
```

---

## 索引管理

### 创建索引

```
POST /api/v1/indices
```

**请求体：**

```json
{
  "name": "my_index",
  "dimension": 1536,
  "similarity": "cosine"
}
```

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `name` | string | 是 | - | 索引名称（最长 128 字符） |
| `dimension` | int | 是 | - | 向量维度（1-4096） |
| `similarity` | string | 否 | `cosine` | 相似度算法：`cosine` / `dot_product` / `l2_norm` |

### 列举索引

```
GET /api/v1/indices
```

**响应：**

```json
{
  "indices": [
    {
      "name": "rag_my_index",
      "display_name": "my_index",
      "docs_count": 1234,
      "store_size": "15mb",
      "health": "green",
      "status": "open"
    }
  ],
  "total": 1
}
```

### 删除索引

```
DELETE /api/v1/indices/{name}
```

**响应：**

```json
{
  "message": "Index deleted successfully",
  "index": "my_index"
}
```

---

## 健康检查

```
GET /healthz
```

**响应：**

```json
{
  "status": "ok"
}
```
