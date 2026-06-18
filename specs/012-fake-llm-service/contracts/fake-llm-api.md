# Contract: Fake LLM OpenAI-Compatible API

**Updated**: 2026-06-18

## Endpoint

`POST /v1/chat/completions`

Implements the subset of the OpenAI Chat Completions API required by `@langchain/openai` `ChatOpenAI` in large tests. Reference: https://platform.openai.com/docs/api-reference/chat/create

## Request

```json
{
  "model": "any-model-name",
  "messages": [
    {"role": "system", "content": "optional"},
    {"role": "user", "content": "hello"}
  ],
  "stream": true
}
```

**Rules**:

- `Authorization: Bearer ...` is accepted but not validated.
- `model` is accepted and ignored.
- `messages` is inspected only to find the last `role: "user"` text content for keyword matching.
- Unknown fields are ignored.
- `stream: true` returns Server-Sent Events; `stream: false` or missing returns one JSON response.

## Streaming response

Content type should be `text/event-stream`.

The service emits chunks compatible with OpenAI chat completion deltas:

```text
data: {"id":"chatcmpl-fake","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-fake","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"thinking text"},"finish_reason":null}]}

data: {"id":"chatcmpl-fake","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"answer text"},"finish_reason":null}]}

data: {"id":"chatcmpl-fake","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

**Rules**:

- The first emitted delta MUST include `"role": "assistant"`. It may be combined with the `reasoning_content` delta (as in the validated prototype `experimental/openai_llm/fake_service/main.go`) or emitted as its own delta. The validated, evidence-backed shape bundles `role` + `reasoning_content` in the first delta, then a `content` delta, then an empty delta with `finish_reason: "stop"`, then `[DONE]`.
- `reasoning_content` chunk precedes `content` for configured messages with reasoning.
- Empty reasoning may omit the reasoning chunk or emit an empty reasoning value; tests should configure non-empty reasoning for thinking-frame scenarios.
- The terminal `[DONE]` event is always emitted in streaming mode.

## Non-streaming response

```json
{
  "id": "chatcmpl-fake",
  "object": "chat.completion",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "reasoning_content": "thinking text",
        "content": "answer text"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 0,
    "completion_tokens": 0,
    "total_tokens": 0
  }
}
```

**Rules**:

- Non-streaming mode includes `reasoning_content` in the message object, mirroring streaming semantics.
- Usage values may be dummy zero values.

## Response selection contract (stateless, per-request)

Each request is matched independently. There is no active group or position counter.

1. Extract the text of the last `role: "user"` message in the request (case-insensitive role). Historical/context messages are not considered.
2. A message matches if ANY of its `match_keywords` is a case-insensitive substring of that text.
3. If one or more messages match: return the message whose `name` sorts first alphabetically (merge order across all files).
4. If ZERO messages match: return a uniformly random message chosen from the full merged set. The service logs a WARN line with the unmatched snippet and chosen message name, e.g. `no keyword matched for "...", returning random message "<name>"`.
   Random fallback is legitimate only for test scenarios that do not assert on response content. Tests requiring specific content must always match a keyword or add new data.

## Error contract

- Malformed template files fail service startup with a clear file/path parse error.
- Empty or missing `match_keywords` on any message → startup failure with error naming the file and field.
- Duplicate message `name` across merged files → startup failure with error naming the conflicting names/files.
- Normal unmatched (no keyword match) requests do not return HTTP errors; they return a random message per the selection contract.
- Unsupported request fields do not return HTTP errors.
