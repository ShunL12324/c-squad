class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.12.6"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.6/csquad_0.12.6_darwin_arm64.tar.gz"
      sha256 "ba76b7ffbeee6e5b405c2f934c71682abcd1daebe82c9a7e5a4a72acb2d72b88"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.6/csquad_0.12.6_darwin_amd64.tar.gz"
      sha256 "a28f75b40831cf8dead3a6ffb5dcc326367ee4fe4c680f2b93a91b1ba1637700"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.6/csquad_0.12.6_linux_arm64.tar.gz"
      sha256 "5dfb63ebb8b8c8d0c867e6ee1c6966117d6e05c018a8d9a3aa844abc4c5c8bee"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.6/csquad_0.12.6_linux_amd64.tar.gz"
      sha256 "bab595224fae99608e5067117e02a005d326b2342c5b1d1098e652a3cb50ee02"
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
