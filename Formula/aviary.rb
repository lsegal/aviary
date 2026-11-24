class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.20"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.20/aviary_v0.7.20_darwin_arm64.tar.gz"
      sha256 "7e69ac130f5c677e38398490a363e878f270ee700e374f0272c54263c724caa2"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.20/aviary_v0.7.20_darwin_amd64.tar.gz"
      sha256 "5f53a4940c4762f5b56e9c4244ef2f9b52f6d1dc851fa35f836866309255a4bd"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.20/aviary_v0.7.20_linux_arm64.tar.gz"
      sha256 "7fb6816114ec490fa1b7839cc26c2813dff660dac2174296563476f0626c8f2d"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.20/aviary_v0.7.20_linux_amd64.tar.gz"
      sha256 "cf0a8407c5bcf4eb736392ee978c9289bce950c4f680be90190d52d54240214a"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
