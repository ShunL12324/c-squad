class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.9.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.9.0/csquad_0.9.0_darwin_arm64.tar.gz"
      sha256 "cd1dfd4fd0f2342e9bb4a718859a55f44c7f83de4acff2f0736534a78e934e55"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.9.0/csquad_0.9.0_darwin_amd64.tar.gz"
      sha256 "51a7ab16eefc4cc923cb6903d2336f2b93c98ac7405faa631a6e4fd3097eab6b"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.9.0/csquad_0.9.0_linux_arm64.tar.gz"
      sha256 "a6cad4f800dcd6ca61fef5f9c232de51ad2c82e1d436d18e12b5f9c3766af6a9"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.9.0/csquad_0.9.0_linux_amd64.tar.gz"
      sha256 "5aba22146abb76a00125db368a4a0400fbb7ac4be3d5a1ed2c62d46e76dd2d48"
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
