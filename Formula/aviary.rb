class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.6.2"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.6.2/aviary_v0.6.2_darwin_arm64.tar.gz"
      sha256 "9690eb0ae2b0089b2c39a546979e50da27dfbb3a23a54339bf29b25c693af4d2"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.6.2/aviary_v0.6.2_darwin_amd64.tar.gz"
      sha256 "9444b22d0489c2d8e5092a5f0c9d9d9d073e0a58f714e054318c3262ef132611"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.6.2/aviary_v0.6.2_linux_arm64.tar.gz"
      sha256 "a1f914302b94ec7bcad53b6b6beebb0c88a91a619ce98ab9f2b3b38efc4bdaa1"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.6.2/aviary_v0.6.2_linux_amd64.tar.gz"
      sha256 "2f06c0e0cccd8993ef87cb5941636ebf5740137e5be361123b94258e758b7b48"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
