class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.6.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.6.0/csquad_0.6.0_darwin_arm64.tar.gz"
      sha256 "8c19097f258fe0bba463dd79b4ebd580b2aa2cb2c50b02c945da1cc3dbe1f789"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.6.0/csquad_0.6.0_darwin_amd64.tar.gz"
      sha256 "36d1335c6d8bbe3429cd4c48e7283400d9e0bf755d4fc113c7655d967299bad0"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.6.0/csquad_0.6.0_linux_arm64.tar.gz"
      sha256 "5d556f919acbc470e05577bd6d84411be24330fd47e51d9dcc87480a8e348fac"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.6.0/csquad_0.6.0_linux_amd64.tar.gz"
      sha256 "5f29ab554ae58167cfc5a1e205d7130829bfe83d71e395fd7e67f557d6857fac"
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
