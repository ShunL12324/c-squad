class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.9.1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.9.1/csquad_0.9.1_darwin_arm64.tar.gz"
      sha256 "ed0bc20d2d4234cef2e48c8a586931de0b6a13c1d2032f68f1144d227479543c"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.9.1/csquad_0.9.1_darwin_amd64.tar.gz"
      sha256 "77ec0dd1d3b55174c6ca232608ef7101aeff162e79232bb75e15b5e14a134534"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.9.1/csquad_0.9.1_linux_arm64.tar.gz"
      sha256 "ea375fa4bd66b1ee2e84d227d0e006799174e9445fa3e74fb95c6feb15955e3c"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.9.1/csquad_0.9.1_linux_amd64.tar.gz"
      sha256 "fc7baf844c217c29b73182107820c1fb4829e19384ca58a259ba6481a9cb7d29"
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
