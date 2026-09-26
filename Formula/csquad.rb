class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.12.7"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.7/csquad_0.12.7_darwin_arm64.tar.gz"
      sha256 "42f12e96d453204c0b1224de48fea725e960386f547db8f6015ec8289932dc9b"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.7/csquad_0.12.7_darwin_amd64.tar.gz"
      sha256 "86f0940f93a07502284fe905f6a69aebf347824e8f342ee92d1595d128cc02e7"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.7/csquad_0.12.7_linux_arm64.tar.gz"
      sha256 "b5f42815dd542a198e558345e5242aa56e3f3b73230aea17429f4bc5344efda0"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.7/csquad_0.12.7_linux_amd64.tar.gz"
      sha256 "16c72b87d14cc34b58ce291f7eeec3629476cf89440ea6344e97bae43859ab3c"
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
