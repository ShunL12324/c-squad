class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.12.10"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.10/csquad_0.12.10_darwin_arm64.tar.gz"
      sha256 "69b72e3364347ce7fac0d46aa897ce2ae1b016b636949e94c898c5094a5776ad"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.10/csquad_0.12.10_darwin_amd64.tar.gz"
      sha256 "09fe84879b63c5ee3f3100da685a0cba7c8d9ebb8a881cf4ed180cdb66f62ab6"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.10/csquad_0.12.10_linux_arm64.tar.gz"
      sha256 "135a859f101473e7a3bda3453663368d80dd87e943839fe66eaaaefac84384fb"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.12.10/csquad_0.12.10_linux_amd64.tar.gz"
      sha256 "26897ff273722e8ad050fd5a093491e26b3d99c1e1503d84c806c4915f2538ff"
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
