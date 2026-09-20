class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.6.1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.6.1/csquad_0.6.1_darwin_arm64.tar.gz"
      sha256 "ae24ffb00db4e6b9e153ea380657408e201e91b10f530140061309d00fcebcd4"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.6.1/csquad_0.6.1_darwin_amd64.tar.gz"
      sha256 "4dea21ef2d9ea16393ba2aa28a107ee33aa04dee35a16630b08a120ac1bfdc84"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.6.1/csquad_0.6.1_linux_arm64.tar.gz"
      sha256 "7e0c872e32371ded23be63f7dba9d2f1e4f70430550ac336a08d8a737ed1b6bc"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.6.1/csquad_0.6.1_linux_amd64.tar.gz"
      sha256 "c9104cd3ca99cef9b5b8d26b7036cd25d66e20281bc77834ce455c5ed2ce2f80"
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
