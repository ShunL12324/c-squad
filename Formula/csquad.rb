class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.12.11"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.11/csquad_0.12.11_darwin_arm64.tar.gz"
      sha256 "d745efcafad051f4fc9623c37e2fad53c5232e834c506b31186e85141808d5a2"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.11/csquad_0.12.11_darwin_amd64.tar.gz"
      sha256 "bb5cb718659b22c2585893d77fa5d454f8b5264f286f17563845eb56311b9a63"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.11/csquad_0.12.11_linux_arm64.tar.gz"
      sha256 "cdab3a9b5ffb3a6d22d90744b8e660f7084190d16c456b6352561d541be8521f"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.11/csquad_0.12.11_linux_amd64.tar.gz"
      sha256 "7d5e978903ad50ad0564336fa73dc47f2c01888bcf37be53ba14b7aeb1781949"
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
