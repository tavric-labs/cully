import Foundation
#if canImport(llama)
import llama
#endif

public enum LocalEngineError: Error { case unavailable, loadFailed, contextLimit, decodeFailed, cancelled }

/// CPU inference remains in process. No prompt or generated text is logged.
public actor LocalEngine {
    public init() {}
    public func complete(modelURL: URL, prompt: String, maxTokens: Int = 160) throws -> String {
        #if canImport(llama)
        try Task.checkCancellation()
        llama_log_set({ _, _, _ in }, nil)
        llama_backend_init()
        var parameters = llama_model_default_params(); parameters.n_gpu_layers = 0
        guard let model = llama_model_load_from_file(modelURL.path, parameters) else { throw LocalEngineError.loadFailed }
        defer { llama_model_free(model) }
        var options = llama_context_default_params()
        options.offload_kqv = false; options.op_offload = false
        options.n_ctx = 4096; options.n_batch = 512
        options.n_threads = Int32(max(1, min(4, ProcessInfo.processInfo.processorCount - 1))); options.n_threads_batch = options.n_threads
        guard let context = llama_init_from_model(model, options), let vocab = llama_model_get_vocab(model) else { throw LocalEngineError.loadFailed }
        defer { llama_free(context) }
        var tokens = [llama_token](repeating: 0, count: prompt.utf8.count + 16)
        let count = llama_tokenize(vocab, prompt, Int32(prompt.utf8.count), &tokens, Int32(tokens.count), false, true)
        guard count > 0, count + Int32(maxTokens) < 4096 else { throw LocalEngineError.contextLimit }
        tokens = Array(tokens.prefix(Int(count)))
        var batch = llama_batch_init(512, 0, 1); defer { llama_batch_free(batch) }
        func add(_ token: llama_token, _ position: Int32, _ logits: Bool) {
            let i = Int(batch.n_tokens)
            batch.token[i] = token; batch.pos[i] = position; batch.n_seq_id[i] = 1
            batch.seq_id[i]![0] = 0; batch.logits[i] = logits ? 1 : 0; batch.n_tokens += 1
        }
        for start in stride(from: 0, to: tokens.count, by: 512) {
            try Task.checkCancellation(); batch.n_tokens = 0
            let end = min(start + 512, tokens.count)
            for i in start..<end { add(tokens[i], Int32(i), i == tokens.count - 1) }
            guard llama_decode(context, batch) == 0 else { throw LocalEngineError.decodeFailed }
        }
        guard let sampler = llama_sampler_init_greedy() else { throw LocalEngineError.loadFailed }
        defer { llama_sampler_free(sampler) }
        var result: [UInt8] = []
        for generated in 0..<maxTokens {
            try Task.checkCancellation()
            let token = llama_sampler_sample(sampler, context, -1)
            if llama_vocab_is_eog(vocab, token) { break }
            var piece = [CChar](repeating: 0, count: 256)
            var n = llama_token_to_piece(vocab, token, &piece, Int32(piece.count), 0, false)
            if n < 0 { piece = [CChar](repeating: 0, count: Int(-n)); n = llama_token_to_piece(vocab, token, &piece, Int32(piece.count), 0, false) }
            guard n >= 0, n <= piece.count else { throw LocalEngineError.decodeFailed }
            result.append(contentsOf: piece.prefix(Int(n)).map { UInt8(bitPattern: $0) })
            batch.n_tokens = 0; add(token, count + Int32(generated), true)
            guard llama_decode(context, batch) == 0 else { throw LocalEngineError.decodeFailed }
        }
        return String(decoding: result, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
        #else
        throw LocalEngineError.unavailable
        #endif
    }
}
