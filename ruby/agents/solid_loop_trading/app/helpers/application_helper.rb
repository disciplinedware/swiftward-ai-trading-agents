module ApplicationHelper
  def format_json_content(str)
    str = str.to_s
    if str.start_with?("{") && str.end_with?("}")
      begin
        JSON.pretty_generate(JSON.parse(str))
      rescue JSON::ParserError
        str
      end
    else
      str
    end
  end
end
