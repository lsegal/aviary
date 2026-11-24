class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.23"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.23/aviary_v0.7.23_darwin_arm64.tar.gz"
      sha256 "10ce67b4c687d968ece85259475daa111d906a30ee81f0868f76874440095431"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.23/aviary_v0.7.23_darwin_amd64.tar.gz"
      sha256 "a1bde9e62a468fe647dbe01e3583fed485014ec2f3e751041525a32b48281c6f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.23/aviary_v0.7.23_linux_arm64.tar.gz"
      sha256 "d4d951b5a9600aafc11e305fd16c4743a902251b1df8b22c3212735ff6c383fb"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.23/aviary_v0.7.23_linux_amd64.tar.gz"
      sha256 "20be96795bf4e55436eb9873b68929ee5aedc08dbed134d3ec55418ca174b7fe"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
