require "json"

namespace :coverage do
  GROUPS = %w[
    app/controllers app/models app/services app/helpers
    app/jobs app/mailers app/queries app/channels
    app/concerns app/views
  ].freeze

  desc "Parse coverage/coverage.json and print summary"
  task :report, [ :sort, :filter, :git ] => :environment do |_t, args|
    CoverageReporter.report(args)
  end

  class CoverageReporter
    def self.report(args)
      new(args).report
    end

    def initialize(args)
      @args = args
    end

    def report
      path = Rails.root.join("coverage", "coverage.json")
      unless File.exist?(path)
        abort "coverage/coverage.json not found. Run tests first."
      end

      data = JSON.parse(File.read(path))
      # simplecov-json 0.2.x: { "files": [{ "filename": "...", "coverage": { "lines": [...] } }] }
      # simplecov-json 0.1.x: { "coverage": { "path" => [...] } }
      raw_coverage = if data["files"].is_a?(Array)
        data["files"].each_with_object({}) do |f, h|
          h[f["filename"]] = f["coverage"]
        end
      else
        data["coverage"]
      end

      git_mode = @args[:git]
      git_changed_lines = {}

      if git_mode
        git_changed_lines = load_git_changes(git_mode)
        if git_changed_lines.empty?
          puts "No changed files found for git mode: #{git_mode}"
          return
        end
      end

      filter = @args[:filter]
      filter_pattern = filter ? Regexp.new(filter, Regexp::IGNORECASE) : nil

      file_stats = []
      total_lines = 0
      covered_lines = 0

      raw_coverage.each do |file_path, line_data|
        next unless file_path.include?("/app/")

        lines = extract_lines(line_data)
        next if lines.nil?

        short = file_path.sub(%r{.*/(?=app/)}, "")

        next if filter_pattern && short !~ filter_pattern
        if git_mode
          next unless git_changed_lines.key?(short)
        end

        if git_mode && git_changed_lines[short]
          changed = git_changed_lines[short]
          relevant = lines.each_with_index.select { |l, i| !l.nil? && changed.include?(i + 1) }
          cov = relevant.count { |l, _| l > 0 }
          miss = relevant.count { |l, _| l == 0 }
          miss_nums = relevant.select { |l, _| l == 0 }.map { |_, i| i + 1 }.sort
          tot = cov + miss
          next if tot == 0
          total_lines += tot
          covered_lines += cov
          pct = (cov.to_f / tot * 100).round(1)
          file_stats << {
            path: short,
            pct: pct,
            covered: cov,
            total: tot,
            missed: miss,
            missed_lines: miss_nums,
            group: detect_group(short),
            changed_only: true
          }
        else
          all_uncovered = lines.each_with_index.select { |l, i| l == 0 }.map { |_, i| i + 1 }.sort
          relevant = lines.compact
          cov = relevant.count { |l| l > 0 }
          miss = relevant.count { |l| l == 0 }
          tot = cov + miss
          next if tot == 0
          total_lines += tot
          covered_lines += cov
          pct = (cov.to_f / tot * 100).round(1)
          file_stats << {
            path: short,
            pct: pct,
            covered: cov,
            total: tot,
            missed: miss,
            missed_lines: all_uncovered,
            group: detect_group(short),
            changed_only: false
          }
        end
      end

      if file_stats.empty?
        puts "No matching files found."
        return
      end

      overall = total_lines > 0 ? (covered_lines.to_f / total_lines * 100).round(2) : 0.0

      header = "Coverage Report"
      header += " [filter: #{filter}]" if filter
      header += " [git: #{git_mode}]" if git_mode

      puts "=" * 60
      puts "  #{header}"
      puts "  #{covered_lines}/#{total_lines} lines (#{overall}%)"
      puts "=" * 60

      sort_mode = (@args[:sort] || "worst").to_sym

      file_stats.group_by { |f| f[:group] }.sort.each do |group_name, files|
        g_total = files.sum { |f| f[:total] }
        g_covered = files.sum { |f| f[:covered] }
        g_pct = g_total > 0 ? (g_covered.to_f / g_total * 100).round(1) : 0.0

        filtered = apply_sort(files, sort_mode)
        next if filtered.empty?

        label = git_mode ? "#{group_name} (changed lines)" : group_name
        puts
        puts "--- #{label} (#{g_pct}% | #{g_covered}/#{g_total}) ---"
        filtered.each do |f|
          tag = f[:changed_only] ? " [changed]" : ""
          puts "  #{f[:pct].to_s.rjust(5)}%  #{f[:path]}#{tag}  (#{f[:missed]} missed of #{f[:total]})"
          next if f[:missed_lines].nil? || f[:missed_lines].empty?
          puts "         uncovered: #{format_line_ranges(f[:missed_lines])}"
        end
      end

      puts
    end

    private

    def extract_lines(line_data)
      case line_data
      when Hash
        line_data["lines"]
      when Array
        line_data
      else
        nil
      end
    end

    def detect_group(short)
      GROUPS.each { |prefix| return prefix if short.start_with?(prefix) }
      "app/other"
    end

    def format_line_ranges(line_nums)
      return "" if line_nums.empty?
      ranges = []
      start = line_nums[0]
      finish = start
      line_nums[1..].each do |n|
        if n == finish + 1
          finish = n
        else
          ranges << range_str(start, finish)
          start = n
          finish = n
        end
      end
      ranges << range_str(start, finish)
      ranges.join(", ")
    end

    def range_str(start, finish)
      start == finish ? start.to_s : "#{start}-#{finish}"
    end

    def apply_sort(files, mode)
      case mode
      when :uncovered
        files.select { |f| f[:pct] == 0.0 }.sort_by { |f| -f[:total] }
      when :partial
        files.select { |f| f[:pct] > 0 && f[:pct] < 100 }.sort_by { |f| f[:pct] }
      when :full
        files.select { |f| f[:pct] == 100.0 }.sort_by { |f| -f[:total] }
      else
        files.sort_by { |f| f[:pct] }
      end
    end

    def load_git_changes(mode)
      result = {}

      case mode
      when "staged"
        diff_output = `git diff --cached --diff-filter=ACMR --unified=0 -- app/`
      when "unstaged"
        diff_output = `git diff --diff-filter=ACMR --unified=0 -- app/`
      when "head", "branch"
        base = mode == "head" ? "HEAD~1" : nil
        if base.nil?
          branch = `git rev-parse --abbrev-ref HEAD`.strip
          base = `git merge-base main #{branch}`.strip rescue nil
          base ||= `git merge-base master #{branch}`.strip rescue nil
        end
        if base && !base.empty?
          diff_output = `git diff #{base}...HEAD --diff-filter=ACMR --unified=0 -- app/`
        else
          diff_output = `git diff HEAD --diff-filter=ACMR --unified=0 -- app/`
        end
      else
        diff_output = `git diff HEAD --diff-filter=ACMR --unified=0 -- app/`
      end

      current_file = nil
      diff_output.each_line do |line|
        if line =~ %r{^\+\+\+ b/(app/.+)$}
          current_file = $1.strip
          result[current_file] ||= []
        elsif line =~ /^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@/
          next unless current_file
          start_line = $1.to_i
          count = ($2 || 1).to_i
          count = 1 if count == 0
          result[current_file].concat((start_line...(start_line + count)).to_a)
        end
      end

      result
    end
  end
end
