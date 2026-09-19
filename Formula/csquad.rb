class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.3.1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.3.1/csquad_0.3.1_darwin_arm64.tar.gz"
      sha256 "8ebeb7d5b3130ed19bb58248f6112a3be752801ebf96ed0a2938607fdb873062"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.3.1/csquad_0.3.1_darwin_amd64.tar.gz"
      sha256 "55821a9b964cbd9d64f594bf7641fc404f7f9fdf8a549d8108417bf167bc66e3"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.3.1/csquad_0.3.1_linux_arm64.tar.gz"
      sha256 "af908c5ff54ccd3bbe6b91c5f99aa8dad4eb0e53f40a8f46d660e536ecfba73a"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.3.1/csquad_0.3.1_linux_amd64.tar.gz"
      sha256 "90c7d5b3c32a8a7029c5756a8c606695c5855ddc63c0dee282b9aff55d23f952"
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
