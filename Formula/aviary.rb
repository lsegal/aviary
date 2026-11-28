# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.12'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.12/aviary_v1.0.12_darwin_arm64.tar.gz'
      sha256 'ee0bbc5c263f1eb9864a7031412d216c002fb6b62677edcd2df45f4c72beafe1'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.12/aviary_v1.0.12_darwin_amd64.tar.gz'
      sha256 'ea1b1bde6ade23ab8d69bb643b7d21856de2435ec61844bb1a79f707cfb213aa'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.12/aviary_v1.0.12_linux_arm64.tar.gz'
      sha256 '28937be16e18e1ebd00d43f060e900932e260563c87c3ade01ad207a2b6c8e7c'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.12/aviary_v1.0.12_linux_amd64.tar.gz'
      sha256 'a7e33dba68007259a12c61023e569c00215910361590a5939eba4bb72f225dbb'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
