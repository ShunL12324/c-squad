class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.12.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.0/csquad_0.12.0_darwin_arm64.tar.gz"
      sha256 "dc2a7958fabe47c345726a1b48b38d901fa413879962c70ccd3d1e9b028a97ef"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.0/csquad_0.12.0_darwin_amd64.tar.gz"
      sha256 "930f3978feb9ee885e8fad0c5465f967ab0cc4241d020a9d4e25cfe7dffb4cd6"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.0/csquad_0.12.0_linux_arm64.tar.gz"
      sha256 "9f7fdcdf4a4e9d5b7bd2079121fee441a48515c54809f971fca5cf7b90254c48"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.0/csquad_0.12.0_linux_amd64.tar.gz"
      sha256 "cf20a7e19fc3c18b5140236144380b6e38ec92666b994642b4001599f3f0cb3a"
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
