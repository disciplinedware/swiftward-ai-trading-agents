class TestTool < ApplicationTool
  FUNCTION_NAME = "test_tool"
  FUNCTION_DESCRIPTION = "Adds two numbers together. Parameters: a (integer), b (integer)."
  FUNCTION_PARAMETERS = {
    type: "object",
    properties: {
      a: { type: "integer" },
      b: { type: "integer" }
    },
    required: [ "a", "b" ]
  }

  def call(tool_call)
    arguments = tool_call.arguments
    a = arguments["a"].to_i
    b = arguments["b"].to_i
    "#{a} + #{b} = #{a + b}"
  end
end
