class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.7.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.7.0/csquad_0.7.0_darwin_arm64.tar.gz"
      sha256 "b7e89a1b64cbf1746a20f26c5e552d7f827777f25ab99add29f87ce24f4c11c7"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.7.0/csquad_0.7.0_darwin_amd64.tar.gz"
      sha256 "6022e3018e1e4358aa391660ac7bf2648bf77508ab2468627d4cf76cb936b55c"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.7.0/csquad_0.7.0_linux_arm64.tar.gz"
      sha256 "139bb61e82ca56e5e1bf01673f62f15f314bf22428abb86bc337bc093cf2d214"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.7.0/csquad_0.7.0_linux_amd64.tar.gz"
      sha256 "9edc0924fa703bfb3d9e1ca48197f116549842f531a0958f982b9a5d341cb394"
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
