class Csquad < Formula
  desc "Coordinate Claude Code and Codex teams in tmux"
  homepage "https://github.com/ShunL12324/c-squad"
  version "0.4.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.4.0/csquad_0.4.0_darwin_arm64.tar.gz"
      sha256 "8a9103da9644a9e5952c0337363b07b478a51b03dc12b92c0384020cf3d491c9"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.4.0/csquad_0.4.0_darwin_amd64.tar.gz"
      sha256 "be2874de79fc0bc48c64fad4914554d59ddf9bfc35d609fb785e23ad80021219"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.4.0/csquad_0.4.0_linux_arm64.tar.gz"
      sha256 "81404e6756b0c3c708d1fb8783e3383df2954542d2afbeb9da1fd262798e4cc1"
    else
      url "https://github.com/ShunL12324/c-squad/releases/download/v0.4.0/csquad_0.4.0_linux_amd64.tar.gz"
      sha256 "88583774459f092e2db551d33bb121c470dbbbe0c0389e75a13be2999d7ebf52"
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
