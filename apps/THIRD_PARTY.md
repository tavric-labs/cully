# Mobile sources and licenses

Cully source remains Apache-2.0; see the root LICENSE and NOTICE. Its mobile clients consume MIT-licensed mcp-mobile 0.1.0, which wraps the official MCP Kotlin SDK 0.15.0 and Swift SDK 0.12.1. The SDK repository lists its transitive dependencies and licenses.

| Component | Source / version | License |
| --- | --- | --- |
| llama.cpp | https://github.com/ggml-org/llama.cpp/releases/tag/b11718 (547208dca250ceb3e4c8615598d043f2b602a527) | MIT |
| whisper.cpp Android executable | https://github.com/ggml-org/whisper.cpp/releases/tag/v1.9.5 (d1be6fde11ac6e0407606b4e42fe72d34add8037) | MIT |
| Qwen 0.5B GGUF | https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF/tree/9217f5db79a29953eb74d5343926648285ec7e67 | Apache-2.0 |
| Whisper tiny Q5_1 weights | https://huggingface.co/ggerganov/whisper.cpp/tree/5359861c739e955e79d9a303bcbc70fb988958b1 | MIT / upstream model license |
| Cully icon | https://cully.net/favicon.svg | Existing Cully brand asset, used unchanged |

Apple runtime ZIP SHA-256: `1c4e5dccf8114c8cbff3280440fa7eef5d58a28745ed6e7f5d73bde335f61c5b`.
Android runtime archive SHA-256: `ebfa0cc03882064d461fae68ac5c9d7e8554be18d35c6ebb7cf63c9f3e563c36`.
Model sizes, pinned URLs and SHA-256 digests are in `models.json`, copied identically into both apps. Neither weights nor OAuth credentials are bundled in source or APKs. Preserve upstream copyright/license notices when rebuilding native dependencies. Bundled license texts live in each app's Brand/ThirdParty resources.
