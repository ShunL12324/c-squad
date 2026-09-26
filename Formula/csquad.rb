class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.12.9"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.9/csquad_0.12.9_darwin_arm64.tar.gz"
      sha256 "ba192843a2f43827159dafc8bac74c44d24062041c611579deecfe0da5a53f69"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.9/csquad_0.12.9_darwin_amd64.tar.gz"
      sha256 "4c644fb65473516ff6f5f91540ae169668ad9107e62c2b0ae3d6acfd000d750e"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.9/csquad_0.12.9_linux_arm64.tar.gz"
      sha256 "8b8786ab271a949e6895a4eadcae584d56c24063d28077021e90cccc7f91b6d0"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.9/csquad_0.12.9_linux_amd64.tar.gz"
      sha256 "2fc03affe36f39b97c9beef4f83d0fa820af292d352c07d3a6f4685d3f6c426d"
    end
  end

  depends_on "tmux"
  depends_on "git"

  def install
    bin.install "csquad"
    bash_completion.install "completions/csquad.bash" => "csquad"
    zsh_completion.install "completions/_csquad"
    fish_completion.install "completions/csquad.fish"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/csquad version")
    assert_match "Usage:", shell_output("#{bin}/csquad --help")
    ENV["CSQUAD_CONFIG"] = (testpath/"config.toml").to_s
    shell_output("#{bin}/csquad config")
    assert_path_exists testpath/"config.toml"
  end
end
