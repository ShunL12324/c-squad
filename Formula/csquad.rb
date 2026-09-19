class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.4.2"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.4.2/csquad_0.4.2_darwin_arm64.tar.gz"
      sha256 "6157cded25a096836cfc5c40d5d3784368161a5f5bb1c9f5f85c55a9a2eec177"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.4.2/csquad_0.4.2_darwin_amd64.tar.gz"
      sha256 "6fe911098cc53ed6746043c71861f6242595e656747dae8ec5b27bcf7a30af60"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.4.2/csquad_0.4.2_linux_arm64.tar.gz"
      sha256 "31d202831c0a80bc3d6333e03e8e3387fd374862a2b65b44100ed5d48d1aabd8"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.4.2/csquad_0.4.2_linux_amd64.tar.gz"
      sha256 "4b0d1405e2ac7f9159b7cc5b6b1d5644a8037595cf6ba8ae80587b5697836859"
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
