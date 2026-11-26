import { createHttpClient, type HttpClientOptions } from './http-client';
import { extractApiError } from './extract-api-error';

export const DEFAULT_NEWHUB_BASE_URL = 'https://api.hanmahong5.com';

export interface ChatMessage {
  role: 'system' | 'user' | 'assistant' | 'tool';
  content: string;
  name?: string;
  tool_call_id?: string;
}

export interface ChatCompletionRequest {
  model: string;
  messages: ChatMessage[];
  temperature?: number;
  max_tokens?: number;
  top_p?: number;
  stream?: boolean;
  user?: string;
}

export interface ChatCompletionChoice {
  index: number;
  message?: ChatMessage;
  delta?: Partial<ChatMessage>;
  finish_reason?: string | null;
}

export interface ChatCompletionUsage {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}

export interface ChatCompletionResponse {
  id: string;
  object: string;
  created: number;
  model: string;
  choices: ChatCompletionChoice[];
  usage?: ChatCompletionUsage;
}

export interface NewHubModelDescriptor {
  id: string;
  object?: string;
  owned_by?: string;
}

export interface NewHubClientOptions extends Omit<HttpClientOptions, 'getToken'> {
  getApiKey?: () => string | null | undefined | Promise<string | null | undefined>;
}

export interface NewHubClient {
  listModels(signal?: AbortSignal): Promise<NewHubModelDescriptor[]>;
  chatCompletions(req: ChatCompletionRequest, signal?: AbortSignal): Promise<ChatCompletionResponse>;
  streamChatCompletions(
    req: Omit<ChatCompletionRequest, 'stream'>,
    signal?: AbortSignal,
  ): AsyncGenerator<ChatCompletionResponse, void, undefined>;
}

/**
 * Parses an OpenAI/NewHub `text/event-stream` byte stream into individual JSON
 * payloads, safely handling multi-byte UTF-8 splits and arbitrary chunk framing.
 */
export async function* parseSSEChunks<T = unknown>(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<T, void, undefined> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let newlineIdx = buffer.indexOf('\n');
      while (newlineIdx !== -1) {
        const rawLine = buffer.slice(0, newlineIdx).replace(/\r$/, '');
        buffer = buffer.slice(newlineIdx + 1);
        if (rawLine.startsWith('data:')) {
          const payload = rawLine.slice(5).trim();
          if (payload === '[DONE]') return;
          if (payload.length > 0) {
            yield JSON.parse(payload) as T;
          }
        }
        newlineIdx = buffer.indexOf('\n');
      }
    }
    const tail = (buffer + decoder.decode()).trim();
    if (tail.startsWith('data:')) {
      const payload = tail.slice(5).trim();
      if (payload.length > 0 && payload !== '[DONE]') {
        yield JSON.parse(payload) as T;
      }
    }
  } finally {
    reader.releaseLock();
  }
}

/**
 * Creates a typed client for Lurus NewHub (`api.hanmahong5.com`), reused across
 * Switch, Creator, Founder, Claude2Master, Fable, and web consoles.
 */
export function createNewHubClient(options: NewHubClientOptions = {}): NewHubClient {
  const http = createHttpClient({
    ...options,
    baseUrl: options.baseUrl ?? DEFAULT_NEWHUB_BASE_URL,
    getToken: options.getApiKey,
  });

  return {
    async listModels(signal?: AbortSignal): Promise<NewHubModelDescriptor[]> {
      const envelope = await http.get<{ data?: NewHubModelDescriptor[] }>('/v1/models', {
        signal,
        fallbackCode: 'newhub_models_failed',
      });
      return Array.isArray(envelope?.data) ? envelope.data : [];
    },

    async chatCompletions(
      req: ChatCompletionRequest,
      signal?: AbortSignal,
    ): Promise<ChatCompletionResponse> {
      return http.post<ChatCompletionResponse>(
        '/v1/chat/completions',
        { ...req, stream: false },
        { signal, fallbackCode: 'newhub_chat_failed' },
      );
    },

    async *streamChatCompletions(
      req: Omit<ChatCompletionRequest, 'stream'>,
      signal?: AbortSignal,
    ): AsyncGenerator<ChatCompletionResponse, void, undefined> {
      const res = await http.requestRaw('/v1/chat/completions', {
        method: 'POST',
        headers: { Accept: 'text/event-stream' },
        body: { ...req, stream: true },
        signal,
        fallbackCode: 'newhub_stream_failed',
      });
      if (!res.body) {
        throw extractApiError(new Error('Empty SSE response body'), 'newhub_empty_stream');
      }
      yield* parseSSEChunks<ChatCompletionResponse>(res.body);
    },
  };
}
