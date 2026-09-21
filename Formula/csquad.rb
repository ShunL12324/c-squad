class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.7.1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.7.1/csquad_0.7.1_darwin_arm64.tar.gz"
      sha256 "c5aa0f5c4e7b0decc1248d08d02ef822edce7d4badc6505daee892a38c43e0b9"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.7.1/csquad_0.7.1_darwin_amd64.tar.gz"
      sha256 "509b79c8f331499af1fc88104e4568d6e6d8da6994cb3c25944dd9d7cc2b2a37"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.7.1/csquad_0.7.1_linux_arm64.tar.gz"
      sha256 "db5cd41d581a4b25efa851cc0cc93c7520e2e4d79dac74b9444cfc96c37e486f"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.7.1/csquad_0.7.1_linux_amd64.tar.gz"
      sha256 "25434adfcecb1af5406661bb6783887c1b54b96c5418ffa33684a50f94d0b983"
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
