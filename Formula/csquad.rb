class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.10.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.10.0/csquad_0.10.0_darwin_arm64.tar.gz"
      sha256 "edef93e7b4532a9202f67bd60383257f1ad2cae137cd2f454e98d2e056e245d1"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.10.0/csquad_0.10.0_darwin_amd64.tar.gz"
      sha256 "97d552edfe098f2b4bc64ae32f690f0b7db2d048458427f8eaaa3a5f8439fa8a"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.10.0/csquad_0.10.0_linux_arm64.tar.gz"
      sha256 "ebec511da8ef90c24af3716805e2ffb09da6adf4d2d0441f859c173fa536ddd7"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.10.0/csquad_0.10.0_linux_amd64.tar.gz"
      sha256 "0c2a9336d1781da19eaec1ebdf3265f1f9b107b62318946f31124785a698ebea"
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
