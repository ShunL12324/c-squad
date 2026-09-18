class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.1.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.1.0/csquad_0.1.0_darwin_arm64.tar.gz"
      sha256 "88fe961a987d0d6c0fca5fabd82434e530972dc5f9ab75b36822a410d983b006"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.1.0/csquad_0.1.0_darwin_amd64.tar.gz"
      sha256 "2b26d7a089273ec33c826d2666c02722fb4f5f11036d98ed1abe9706954d15a5"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.1.0/csquad_0.1.0_linux_arm64.tar.gz"
      sha256 "790e9fd3c8b9149cd3d28e842b2e5a48b5e9683da0f047e7cd206e5a2435aa1c"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.1.0/csquad_0.1.0_linux_amd64.tar.gz"
      sha256 "52a30d18b41c2ed5e4478e4e283d4c8d4c0161af594285c52b79645a577fa00c"
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
