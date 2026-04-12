class ApplicationTool
  FUNCTION_NAME = "override_me"
  FUNCTION_DESCRIPTION = "override_me"
  FUNCTION_PARAMETERS = {
    type: "object",
    properties: {}
  }

  def self.to_tool_spec
    {
      type: "function",
      function: {
        name: self::FUNCTION_NAME,
        description: self::FUNCTION_DESCRIPTION,
        parameters: self::FUNCTION_PARAMETERS
      }
    }
  end

  def call(tool_call)
    raise NotImplementedError, "#{self.class.name}#call is not implemented"
  end

  def apply_defaults(arguments)
    hydrated = (arguments || {}).dup
    params = self.class::FUNCTION_PARAMETERS
    properties = (params[:properties] || params["properties"] || {})

    properties.each do |key, schema|
      schema = schema.with_indifferent_access if schema.is_a?(Hash)
      if schema.key?(:default) && !hydrated.key?(key.to_s) && !hydrated.key?(key.to_sym)
        hydrated[key.to_s] = schema[:default]
      end
    end

    hydrated
  end
end
