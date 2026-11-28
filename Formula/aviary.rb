# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.10'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.10/aviary_v1.0.10_darwin_arm64.tar.gz'
      sha256 '7134898364329bbea00e5098328def7c9dd505c3c3d6822168a30fca159db3db'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.10/aviary_v1.0.10_darwin_amd64.tar.gz'
      sha256 'f65ff23841af8f6a4d430ed3ef26ee8872090fc2f57c4aef54d88613e8569329'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.10/aviary_v1.0.10_linux_arm64.tar.gz'
      sha256 '46040a974ff06e17da953dc00ff1db897eb16af5de0c1b5b6cea33c0acb06c2c'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.10/aviary_v1.0.10_linux_amd64.tar.gz'
      sha256 'ba1bc5ed870f8f23d134628b1a8f88f291dadfc3ab206bb1d2cd4a2f7efdde58'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
