class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.12.4"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.4/csquad_0.12.4_darwin_arm64.tar.gz"
      sha256 "7a945caa0ddf09d52ac2e28b05ab5f3d140da7f4f87937f37d5d07e3f11e6103"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.4/csquad_0.12.4_darwin_amd64.tar.gz"
      sha256 "702507e3e8c918fc2126365bd86c1a17b5f219a1b211494b6358d0d0aeffadaf"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.4/csquad_0.12.4_linux_arm64.tar.gz"
      sha256 "efca98e36c5a59e6afa2080216887e442ad3f5752c429b96cf659a98406c111c"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.4/csquad_0.12.4_linux_amd64.tar.gz"
      sha256 "cb8bc062896b6558ff9692323a9277c3f141df2324a45a0839a0707a839cc8eb"
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
