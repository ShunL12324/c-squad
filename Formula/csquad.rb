class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.12.1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.1/csquad_0.12.1_darwin_arm64.tar.gz"
      sha256 "c6802c5758a85a75e6768e2b5e7fa269f455a51bd41aee87e3b7055cb4683c2f"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.1/csquad_0.12.1_darwin_amd64.tar.gz"
      sha256 "32ba69f9871a3713b9c4b3bb471c651b02fd9f09c2a239abe24b5556fe38a1a7"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.1/csquad_0.12.1_linux_arm64.tar.gz"
      sha256 "1b03189dbce3fb00d455980f5bacb6775db0baf5416579a85e2a16309fca03bc"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.1/csquad_0.12.1_linux_amd64.tar.gz"
      sha256 "8491de6b3873561380f1662805e4a9d0dd5eedcc8923727e10a8e570309d2445"
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
