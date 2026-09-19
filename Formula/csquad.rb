class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.3.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.3.0/csquad_0.3.0_darwin_arm64.tar.gz"
      sha256 "280277c1116959149e00864c41d2198dd614dce1a6411aca95f864c4f07eb69a"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.3.0/csquad_0.3.0_darwin_amd64.tar.gz"
      sha256 "6b5441c034ab17ac6724699376f14dbf2ae1db3cc7af92840fbb42a20b3c72a6"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.3.0/csquad_0.3.0_linux_arm64.tar.gz"
      sha256 "f41d745aa59ad9907abd92d0d956519f712bd897b751a1a76ede9b22131513ba"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.3.0/csquad_0.3.0_linux_amd64.tar.gz"
      sha256 "d9bf1aefe548cdb2a068cb06b3a8d7bd3c0df63d9866cd6edd696cebb9fc2490"
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
