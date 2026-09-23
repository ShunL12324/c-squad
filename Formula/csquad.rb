class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.11.1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.11.1/csquad_0.11.1_darwin_arm64.tar.gz"
      sha256 "f81fa37900d1748eb693cd246afc1f67b6db1368b49c908dd1c7814ec80ef859"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.11.1/csquad_0.11.1_darwin_amd64.tar.gz"
      sha256 "2a4150fdc55ddd1239cb86006b1a6fa21916a6869e0d13da3d6686a8c2897328"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.11.1/csquad_0.11.1_linux_arm64.tar.gz"
      sha256 "40ec74ef16f2f571b8819c3214da44f281d8c609fce07f8eebce504f87523e8a"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.11.1/csquad_0.11.1_linux_amd64.tar.gz"
      sha256 "71dfff2fc67e887f0332f46e921838afedf3a0d8381b60ad01e5eaa0960b37bf"
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
