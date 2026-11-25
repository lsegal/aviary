class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.38"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.38/aviary_v0.7.38_darwin_arm64.tar.gz"
      sha256 "30dceb2a1dc836f5c57bc43d86c31bcd31949120780e508a17ca1a11aef3504c"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.38/aviary_v0.7.38_darwin_amd64.tar.gz"
      sha256 "004c4f3d4e40eb431a275a94a4bb2e856e7a06509dd1b91d00f7370a7b1323ae"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.38/aviary_v0.7.38_linux_arm64.tar.gz"
      sha256 "501a037752142282a71faf33d6769fc01359671ed4c47d4c6cdff1b1e1693250"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.38/aviary_v0.7.38_linux_amd64.tar.gz"
      sha256 "14099805af8e3a293230e7559748349fee15c4c5faa9542f9b2e55ea5c87b2a0"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
