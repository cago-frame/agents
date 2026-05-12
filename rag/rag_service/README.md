# RAG Service

RAG (Retrieval-Augmented Generation) 检索增强生成微服务，为 Cago Agent 框架提供文档解析、文本分块、向量嵌入生成和语义搜索等核心能力。

## 功能特性

- **文档解析** - 支持 PDF、DOCX、HTML、TXT/CSV/Markdown 等多种格式
- **文本分块** - 提供递归字符分块和基于 Token 的分块两种策略
- **向量嵌入** - 支持 OpenAI、Sentence Transformers、HuggingFace 等多种 Embedding 提供商
- **向量存储** - 支持 Elasticsearch（生产环境）和 ChromaDB（本地轻量级）两种后端
- **语义搜索** - 基于向量相似度的 kNN 检索，支持元数据过滤
- **本地扫描** - CLI 命令一键扫描本地目录，批量导入文件到向量数据库
- **RESTful API** - 基于 FastAPI 的异步高性能接口，自带 Swagger 文档

## 快速开始

### 安装

```bash
# 基础安装（使用 Elasticsearch + OpenAI Embedding）
pip install .

# 包含本地 Embedding（Sentence Transformers）和 ChromaDB 支持
pip install ".[local]"

# 安装开发依赖
pip install ".[dev]"
```

### 配置

复制示例配置文件并根据实际环境修改，配置项说明详见 [`config.example.yaml`](config.example.yaml)：

```bash
cp config.example.yaml config.yaml
```

常用环境变量覆盖：

| 环境变量 | 说明 |
|---------|------|
| `OPENAI_API_KEY` | OpenAI API 密钥 |
| `OPENAI_BASE_URL` | OpenAI API 地址 |
| `RAG_EMBEDDING_PROVIDER` | Embedding 提供商 |
| `RAG_VECTORSTORE_PROVIDER` | 向量存储后端 |
| `ELASTICSEARCH_PASSWORD` | Elasticsearch 密码 |
| `ELASTICSEARCH_HOSTS` | Elasticsearch 地址（逗号分隔） |
| `ELASTICSEARCH_CA_CERTS` | Elasticsearch CA 证书路径 |

### 启动服务

```bash
rag-service              # 启动 HTTP 服务器（默认）
rag-service serve        # 同上
```

服务启动后访问：

- API 地址：`http://localhost:8000`
- Swagger 文档：`http://localhost:8000/docs`
- 健康检查：`http://localhost:8000/healthz`

### 扫描本地目录

使用 `scan` 子命令将本地文件批量导入向量数据库：

```bash
# 扫描目录（使用配置文件中的向量存储后端）
rag-service scan ./my-documents

# 使用 ChromaDB 本地向量库（无需 Elasticsearch）
rag-service scan ./my-documents --vectorstore-provider chroma

# 自定义索引名和存储位置
rag-service scan ./papers --index-name research --vectorstore-provider chroma --chroma-dir ./vectors

# 只扫描 PDF 文件
rag-service scan ./docs --glob "*.pdf"

# 预览模式 - 只列出文件不执行处理
rag-service scan ./docs --dry-run
```

**scan 命令参数：**

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `directory` | 扫描的目录路径（必填） | - |
| `--index-name` | 目标索引/集合名称 | `documents` |
| `--vectorstore-provider` | 向量存储后端 (`elasticsearch` / `chroma`) | 配置文件中的值 |
| `--chroma-dir` | ChromaDB 持久化目录 | `./.chroma_data` |
| `--glob` | 文件过滤模式（如 `*.pdf`） | 全部支持的类型 |
| `--dry-run` | 只列出文件不执行处理 | `false` |
| `--config` | 配置文件路径 | `config.yaml` |

扫描时会自动跳过隐藏文件/目录及 `.git`、`__pycache__`、`node_modules` 等目录。

## Docker 部署

```bash
# 构建镜像
docker build -t rag-service:latest .

# 使用 Docker Compose（包含 Elasticsearch）
docker-compose up -d
```

`docker-compose.yml` 将启动 Elasticsearch 8.17.0（端口 9200）和 RAG Service（端口 8000）。

## API 接口

所有业务接口以 `/api/v1` 为前缀。详细的请求/响应参数说明请参考 [`API.md`](API.md) 或 Swagger 文档（`/docs`）。

| 接口 | 方法 | 说明 |
|------|------|------|
| `/api/v1/embeddings` | POST | 生成向量嵌入 |
| `/api/v1/documents` | POST | 上传文档（multipart/form-data） |
| `/api/v1/documents` | DELETE | 删除文档 |
| `/api/v1/search` | POST | 语义搜索 |
| `/api/v1/indices` | POST | 创建索引 |
| `/api/v1/indices` | GET | 列举所有索引 |
| `/api/v1/indices/{name}` | DELETE | 删除索引 |
| `/healthz` | GET | 健康检查 |

## 支持的文件格式

| 格式 | MIME 类型 | 解析库 |
|-----|----------|--------|
| 纯文本 / CSV / Markdown | `text/plain`, `text/csv`, `text/markdown` | 内置 |
| PDF | `application/pdf` | PyMuPDF |
| DOCX | `application/vnd.openxmlformats-officedocument.wordprocessingml.document` | python-docx |
| HTML | `text/html`, `application/xhtml+xml` | BeautifulSoup4 |

## 开发

```bash
# 运行测试
pytest tests/

# 代码检查
ruff check rag_service/
ruff format rag_service/
```
