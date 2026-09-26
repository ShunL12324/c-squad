class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.12.8"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.8/csquad_0.12.8_darwin_arm64.tar.gz"
      sha256 "8e4ae35b131c79cecf7e5e41e3a46176023666b6fdb5273a3c4e1e155d644ccd"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.8/csquad_0.12.8_darwin_amd64.tar.gz"
      sha256 "65b749951be1b90d946197699d2bf23d71bb34d0e8c76c0dfca79fe8384bc36b"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.8/csquad_0.12.8_linux_arm64.tar.gz"
      sha256 "8abb231bae0dedc85cd3521b5068f857adff8714843a51b088a0292a91d12a18"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.8/csquad_0.12.8_linux_amd64.tar.gz"
      sha256 "7733d4eba3ef63fb2054bff581f477bed18b861ec55edecf279469f6c21f0377"
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
