class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.33"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.33/aviary_v0.7.33_darwin_arm64.tar.gz"
      sha256 "494340d5b868c039b12526fe97b0f4c2d0ca12397664cb99dc15630fec5a9480"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.33/aviary_v0.7.33_darwin_amd64.tar.gz"
      sha256 "9dcf9941da51ac4e995235383bed49afaf2f9318b1d41adcb5aa676890ffe465"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.33/aviary_v0.7.33_linux_arm64.tar.gz"
      sha256 "d8c8c5be08dcd407208e99fcbdcd2685ab73d66e02670c41fd21bb6b3a768494"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.33/aviary_v0.7.33_linux_amd64.tar.gz"
      sha256 "3121be87f5fbc9f789acba7276db8db17dcf363d13079fa76af6820a9f071d83"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
