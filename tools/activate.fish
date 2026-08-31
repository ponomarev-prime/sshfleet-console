set -l script_path (status --current-filename)
set -l tools_dir (path resolve (path dirname "$script_path"))

if not contains -- "$tools_dir/bin" $PATH
    set -gx PATH "$tools_dir/bin" $PATH
end
