class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.8.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.8.0/csquad_0.8.0_darwin_arm64.tar.gz"
      sha256 "f687faecbf6245b262a44fb7e12f6ea7fa7acc97a40904eda49bed910d924700"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.8.0/csquad_0.8.0_darwin_amd64.tar.gz"
      sha256 "0cfc3b8f9829fc4d85383b1f3e462a55a516128febd734840f0f2a9765eb9c97"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.8.0/csquad_0.8.0_linux_arm64.tar.gz"
      sha256 "d2b0623d2f8146bafe845e0f154f688da5127495532181a70b91b7a674517e37"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.8.0/csquad_0.8.0_linux_amd64.tar.gz"
      sha256 "85929885ed3860623c80e6bf3eb9e61293f815ace39ea70d6ffcafcefb2fb338"
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
