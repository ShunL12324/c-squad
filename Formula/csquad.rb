class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.4.3"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.4.3/csquad_0.4.3_darwin_arm64.tar.gz"
      sha256 "8e1346195e3a79af69c07e7d386ee8f8c0c80a1fea802cb32906b510b6918b12"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.4.3/csquad_0.4.3_darwin_amd64.tar.gz"
      sha256 "277c0c41f8095d60166fb410a479a6e43c232ef901505b19952aeefa995d153c"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.4.3/csquad_0.4.3_linux_arm64.tar.gz"
      sha256 "58dda1fb9c969cfbce20cfa125ed6887f70f18736d8d466dc3889bddf6ad1c6d"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.4.3/csquad_0.4.3_linux_amd64.tar.gz"
      sha256 "f65107d3a40b272d05785bada40959b8121f13f2f1c24ecbe4b2b151572c4815"
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
