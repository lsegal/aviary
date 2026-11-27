class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.84"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.84/aviary_v0.7.84_darwin_arm64.tar.gz"
      sha256 "ae22386f921829f23c8217ddd8163917c035da4db00eb3d3e441299b41ff2b07"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.84/aviary_v0.7.84_darwin_amd64.tar.gz"
      sha256 "2fac79fab2912a336cb5cddd537615d47c2304211bea7e08536647e2aef9dc3f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.84/aviary_v0.7.84_linux_arm64.tar.gz"
      sha256 "62f0f1fcc362cae071c1328219281208857f3da1dd644c55a1798c12ff69377a"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.84/aviary_v0.7.84_linux_amd64.tar.gz"
      sha256 "610a939ede8439e1bca330b7708bd7be9e75b01e616158479bacd4e23fcea8c2"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
