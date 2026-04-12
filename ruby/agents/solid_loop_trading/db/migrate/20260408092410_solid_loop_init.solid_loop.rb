# This migration comes from solid_loop (originally 20260408000100)
class SolidLoopInit < ActiveRecord::Migration[8.1]
  def change
    create_table :solid_loop_loops do |t|
      t.references :subject, polymorphic: true

      t.string :title
      t.string :agent_class_name
      t.string :status, default: "init", null: false

      t.jsonb :state, default: {}, null: false

      t.integer :step_count, default: 0, null: false
      t.integer :tokens_prompt, default: 0, null: false
      t.integer :tokens_completion, default: 0, null: false
      t.integer :tokens_total, default: 0, null: false
      t.integer :tokens_prompt_cached, default: 0, null: false

      t.float :duration_parsing, default: 0.0, null: false
      t.float :duration_generation, default: 0.0, null: false
      t.float :duration_tools, default: 0.0, null: false
      t.float :duration_total, default: 0.0, null: false

      t.decimal :cost, precision: 15, scale: 10, default: 0.0, null: false

      t.text :error_message

      t.timestamps
    end
    add_index :solid_loop_loops, :status

    create_table :solid_loop_mcp_sessions do |t|
      t.references :loop, null: false, foreign_key: { to_table: :solid_loop_loops }

      t.string :mcp_name, null: false
      t.string :session_id, null: false

      t.jsonb :metadata, default: {}, null: false

      t.timestamps
    end
    add_index :solid_loop_mcp_sessions, [:loop_id, :mcp_name], unique: true

    create_table :solid_loop_mcp_tools do |t|
      t.references :mcp_session, null: false, foreign_key: { to_table: :solid_loop_mcp_sessions }

      t.string :name, null: false
      t.jsonb :input_schema, default: {}, null: false
      t.text :description

      t.timestamps
    end
    add_index :solid_loop_mcp_tools, [:mcp_session_id, :name], unique: true

    create_table :solid_loop_messages do |t|
      t.references :loop, null: false, foreign_key: { to_table: :solid_loop_loops }

      t.string :role, null: false
      t.string :status, null: false
      t.string :tool_call_id

      t.jsonb :tool_calls_raw, default: [], null: false
      t.jsonb :metadata, default: {}, null: false

      t.integer :tokens_prompt, default: 0, null: false
      t.integer :tokens_completion, default: 0, null: false
      t.integer :tokens_total, default: 0, null: false
      t.integer :tokens_prompt_cached, default: 0, null: false

      t.float :tps, default: 0.0, null: false
      t.float :ttft, default: 0.0, null: false
      t.float :duration_thinking, default: 0.0, null: false
      t.float :duration_generation, default: 0.0, null: false

      t.decimal :cost, precision: 15, scale: 10, default: 0.0, null: false

      t.boolean :is_hidden, default: false, null: false

      t.text :content
      t.text :reasoning_content
      t.text :error_message

      t.timestamps
    end
    add_index :solid_loop_messages, :tool_call_id
    add_index :solid_loop_messages, :role
    add_index :solid_loop_messages, [:loop_id, :is_hidden]

    create_table :solid_loop_tool_calls do |t|
      t.references :message, null: false, foreign_key: { to_table: :solid_loop_messages }
      t.references :mcp_session, foreign_key: { to_table: :solid_loop_mcp_sessions }

      t.string :tool_call_id, null: false
      t.string :function_name, null: false

      t.jsonb :arguments, default: {}, null: false
      t.jsonb :metadata, default: {}, null: false

      t.float :duration, default: 0.0, null: false
      t.boolean :is_success

      t.text :result
      t.text :error_message

      t.timestamps
    end
    add_index :solid_loop_tool_calls, [:message_id, :tool_call_id], unique: true
    add_index :solid_loop_tool_calls, :metadata, using: :gin, opclass: :jsonb_path_ops

    create_table :solid_loop_events do |t|
      t.references :eventable, polymorphic: true, null: false
      t.references :loop
      t.references :message

      t.string :name, null: false
      t.string :http_method, null: false
      t.string :tool_call_id
      t.string :url

      t.jsonb :request_data, default: {}, null: false
      t.jsonb :response_data, default: {}, null: false
      t.jsonb :headers, default: {}, null: false

      t.float :duration, default: 0.0, null: false
      t.text :log

      t.timestamps
    end
    add_index :solid_loop_events, :tool_call_id
  end
end
