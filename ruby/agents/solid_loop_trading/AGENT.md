# LLM Agent Documentation

This system is designed to facilitate LLM interactions, tool usage, and event logging within this Rails application.

## Core Models

### 1. `AgentModel`
Stores the configuration for an LLM provider.
- `name`: Human-readable name for the agent profile.
- `base_url`: API endpoint (e.g., OpenAI, Anthropic, or local Llama).
- `api_token`: Authentication token.
- `llm_model_name`: The specific model identifier (e.g., `gpt-4o`, `llama-3`).
- `data`: JSONB field for miscellaneous settings.

### 2. `AgentConversation`
Represents a single chat session or thread.
- `title`: Title of the conversation.
- `agent_model`: Reference to the `AgentModel` used.
- `data`: JSONB field for session-specific state.
- `status`: Enum for conversation lifecycle:
    - `idle`: Initial state or no activity.
    - `processing`: Background job (LLM or Tool) is running.
    - `waiting_for_user`: Agent finished its turn and is waiting for user input.

### 3. `AgentMessage`
Represents individual messages in a conversation.
- `role`: Role of the sender (e.g., `system`, `user`, `assistant`, `tool`).
- `content`: Text content of the message.
- `agent_tool_call`: Optional reference to an `AgentToolCall` (used when `role` is `tool`).
- `status`: Enum for tracking message lifecycle:
    - `success`: Message received or sent successfully.
    - `processing`: Agent is currently working on this message (e.g., running a tool).
    - `pending`: Scheduled for LLM but response is not yet ready.
    - `waiting`: Sent to LLM, waiting for response.
- `data`: JSONB for future metadata.

### 4. `AgentToolCall`
Records specific tool calls initiated by the LLM.
- `tool_call_id`: The ID provided by the LLM for this call.
- `function_name`: Name of the function being called.
- `arguments`: JSONB containing the arguments for the function.
- `result`: The output/result of the tool execution.

### 5. `AgentEvent`
A polymorphic logging model for audit trails and debugging.
- `eventable`: Polymorphic reference (can be `AgentMessage`, `AgentToolCall`, etc.).
- `name`: Enum (`llm_completion`, `tool_call`).
- `method`, `url`, `headers`: HTTP details if the event involved a remote call.
- `request_data`, `response_data`: JSONB snapshots of the payload.
- `log`: Plain text logs.

## Usage Patterns

### Typical Flow
1. Select an `AgentModel`.
2. Find or create an `AgentConversation`.
3. Create a `user` message in the conversation.
4. Set status to `pending` or `waiting`.
5. Upon LLM response, create an `assistant` message.
6. If the LLM requests a tool call:
    - Create an `AgentToolCall` record.
    - Create a `tool` role `AgentMessage` linked to that tool call.
    - Log the request/response in `AgentEvent`.

### Polymorphic Events
Use `AgentEvent` to track raw API interactions. This allows us to debug exactly what was sent to and received from the LLMs or Tools without cluttering the main conversation models.

### Defining Tools

Tools are defined as classes inheriting from `ApplicationTool` in `app/tools/`. Each tool must define its specification and implement a `call` method.

Example: `app/tools/get_script_result.rb`

```ruby
class GetScriptResult < ApplicationTool
  FUNCTION_NAME = "get_script_result"
  FUNCTION_DESCRIPTION = "Returns lines from STDOUT output of the last executed script..."
  FUNCTION_PARAMETERS = {
    type: "object",
    properties: {
      offset: {
        type: "integer",
        description: "Line offset (0-based) to start reading from",
        default: 0
      },
      count: {
        type: "integer",
        description: "Number of lines to read (1-100)",
        default: 20
      }
    }
  }

  def call(arguments)
    offset = arguments["offset"] || 0
    count = arguments["count"] || 20
    # Implementation logic here...
    "Result from line #{offset} to #{offset + count}"
  end
end
```
### Tool Configuration & Loading

Tools are explicitly configured on each `AgentModel` using the `tools` column (a JSONB array of tool class names).

To enable tools for an agent:
1. Define the tool class in `app/tools/` (inheriting from `ApplicationTool`).
2. Add the tool class name (e.g., `"GetScriptResult"`) to the `AgentModel#tools` array.

When a conversation starts, `LlmCompletionJob` uses this list to:
1. Load tool specifications for the LLM.
2. Ensure only authorized tools are executed during `ToolExecutionJob`.

The system uses `ActiveJob` and ActiveRecord callbacks to handle LLM interactions and tool executions asynchronously.

### Overview of Flow

1.  **Conversation Initialization**: User creates an `AgentConversation`. (No callbacks).
2.  **Configuration**: User adds a `system` message to the conversation. (No callbacks for `system` role).
3.  **User Input**: User creates an `AgentMessage` with role `user`.
    -   **Callback**: `after_create_commit` on `AgentMessage` triggers `AgentMessage#schedule_llm_processing`.
    -   **Action**: Schedules `LlmCompletionJob` if the role is `user`.
4.  **LLM Processing**: `LlmCompletionJob` runs.
    -   Collects conversation history.
    -   Sends request to the LLM (configured in `AgentModel`).
    -   Receives response.
5.  **LLM Response**:
    -   If it's a plain text response: Creates a new `AgentMessage` with role `assistant`.
    -   If it requires **Tool Calls**:
        -   Creates `AgentToolCall` records.
        -   Creates an `AgentMessage` with role `assistant` containing the tool calls.
        -   **Callback**: `after_create_commit` on `AgentMessage` (role `assistant` with tool calls) triggers `AgentMessage#schedule_tool_execution`.
        -   **Action**: Schedules `ToolExecutionJob` for each tool call.
6.  **Tool Execution**: `ToolExecutionJob` runs.
    -   Executes the requested function.
    -   Updates `AgentToolCall#result`.
    -   Creates an `AgentMessage` with role `tool` containing the result.
    -   **Callback**: `after_create_commit` on `AgentMessage` (role `tool`) checks if all tool calls in the sequence are finished.
    -   **Action**: If all tools for the current turn are finished, schedules a new `LlmCompletionJob` to send results back to the LLM.
7.  **Completion**: If the LLM returns a final response (no more tool calls), a callback or the job itself marks the agent's work as completed for this turn.

---

## Job Classes

### `LlmCompletionJob`
Responsible for the "Think" phase.
- **Trigger**: New `user` message or completion of all `tool` results.
- **Responsibilities**:
    - Prepare prompt context.
    - Call LLM API.
    - Create `assistant` messages.
    - Log `AgentEvent` for the API call.

### `ToolExecutionJob`
Responsible for the "Act" phase.
- **Trigger**: `assistant` message containing tool calls.
- **Responsibilities**:
    - Find the `AgentToolCall`.
    - Dispatch to the actual Ruby code/service.
    - Update `AgentToolCall` with `result`.
    - Create `tool` role message.
    - Log `AgentEvent` for the tool execution.

---

## Callbacks Logic (Pseudo-code)

```ruby
class AgentMessage < ApplicationRecord
  after_create_commit :process_message

  private

  def process_message
    case role
    when "user"
      LlmCompletionJob.perform_later(agent_conversation_id)
    when "assistant"
      if tool_calls?
        schedule_tool_execution
      else
        notify_agent_completed
      end
    when "tool"
      if conversation.all_tools_finished?
        LlmCompletionJob.perform_later(agent_conversation_id)
      end
    end
  end
```

---

## Тестирование моделей (Model Testing)

При написании тестов на поведение моделей (агентов) необходимо придерживаться следующих строгих правил:

1.  **Планирование диалога**: Заранее готовим полный план работы лупа. Используйте `StreamingLlmEmulator` для формирования ответов модели и `McpEmulator` (или аналоги) для ответов инструментов.
2.  **Завершенность**: Убеждайтесь, что в сценарии отсутствуют бесконечные рекурсии и представлен законченный, лаконичный диалог (от промпта до финального ответа).
3.  **Органическое выполнение**: **Никогда** не используйте `ActiveJob::Base.queue_adapter = :test` в E2E тестах. Все фоновые задачи (`LlmCompletionJob`, `ToolExecutionJob`) должны выполняться органически (в inline режиме или через реальную очередь), чтобы не нарушать цепочку обратных вызовов (callbacks).
4.  **Точка входа**: Тест должен инициироваться через создание `AgentBenchmark` (который создаст `Loop`), после чего вызывается `execution.agent.resume!`. После этого вся цепочка (LLM -> Tool -> LLM) должна пройти автоматически.
5.  **Верификация**: После завершения работы агента проводится фаза проверки:
    -   Содержимое сообщений в базе данных (`SolidLoop::Message`).
    -   Корректность и последовательность запросов, зафиксированных в эмуляторах.
    -   Метаданные и статусы объектов.
