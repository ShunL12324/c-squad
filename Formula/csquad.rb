class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.12.5"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.5/csquad_0.12.5_darwin_arm64.tar.gz"
      sha256 "585dfd2905b081bc7f9c4d0b0ae9f1d53149c57c1fe3eca3f387cf9e681016f7"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.5/csquad_0.12.5_darwin_amd64.tar.gz"
      sha256 "67640478a8f9c977112adaaa32268b04776b268515126d3a02f31f520f673c84"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.5/csquad_0.12.5_linux_arm64.tar.gz"
      sha256 "cfbb41fcd9fb9d50f0485cbec2acae25e08e1f8f21c176ef565552f1079679c8"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.5/csquad_0.12.5_linux_amd64.tar.gz"
      sha256 "d1a09c22fd5c59423ea48b32bfbc759565e357a1b3037ed7879236d035887a51"
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
